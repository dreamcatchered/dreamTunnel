package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Per-device tokens.
//
// Every device that runs the client receives its own token the first time it
// connects. The token compiled into the binary is only a bootstrapping
// credential: it authorises the first enrollment, nothing more. As a result
// devices stay distinguishable in the logs and the admin panel, and a single
// device can be revoked without touching the others.

const (
	// deviceTokenPrefix marks tokens issued by the store: dtd_<id>_<secret>.
	deviceTokenPrefix = "dtd_"

	// defaultEnrollPerHour caps enrollments coming from one IP address.
	defaultEnrollPerHour = 20
	// defaultDeviceLimit caps how many devices one server hands out tokens to.
	defaultDeviceLimit = 5000
)

// Device is one issued token. The token itself is never stored, only its hash.
type Device struct {
	ID       string    `json:"id"`
	Hash     string    `json:"hash"`
	Label    string    `json:"label,omitempty"`
	Platform string    `json:"platform,omitempty"`
	IP       string    `json:"ip,omitempty"`
	First    time.Time `json:"first"`
	Last     time.Time `json:"last"`
	Revoked  bool      `json:"revoked,omitempty"`
}

type deviceFile struct {
	Version int       `json:"version"`
	Devices []*Device `json:"devices"`
}

// DeviceStore keeps the issued device tokens, persisted as JSON.
type DeviceStore struct {
	mu        sync.Mutex
	path      string
	devices   map[string]*Device
	order     []string // enrollment order, oldest first
	lastSave  time.Time
	enrollLog map[string][]time.Time

	perHour int
	limit   int

	// seams for tests
	now      func() time.Time
	randRead func([]byte) (int, error)
}

// NewDeviceStore returns an empty in-memory store.
func NewDeviceStore(path string) *DeviceStore {
	return &DeviceStore{
		path:      path,
		devices:   map[string]*Device{},
		enrollLog: map[string][]time.Time{},
		perHour:   defaultEnrollPerHour,
		limit:     defaultDeviceLimit,
		now:       time.Now,
		randRead:  rand.Read,
	}
}

// LoadDeviceStore reads the store from path; a missing file is not an error.
func LoadDeviceStore(path string) (*DeviceStore, error) {
	s := NewDeviceStore(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var f deviceFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("devices file %s: %w", path, err)
	}
	for _, d := range f.Devices {
		if d == nil || d.ID == "" || d.Hash == "" {
			continue
		}
		s.devices[d.ID] = d
		s.order = append(s.order, d.ID)
	}
	return s, nil
}

// Path is where the store is persisted.
func (s *DeviceStore) Path() string { return s.path }

// ---------------------------------------------------------------- persistence

func (s *DeviceStore) saveLocked() error {
	f := deviceFile{Version: 1, Devices: make([]*Device, 0, len(s.order))}
	for _, id := range s.order {
		if d := s.devices[id]; d != nil {
			f.Devices = append(f.Devices, d)
		}
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(s.path, append(data, '\n'), 0600); err != nil {
		return err
	}
	s.lastSave = s.now()
	return nil
}

// Save writes the store to disk.
func (s *DeviceStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

// ------------------------------------------------------------------ issuance

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// Enroll issues a fresh token for one device.
func (s *DeviceStore) Enroll(label, platform, ip string) (*Device, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.limit > 0 && len(s.devices) >= s.limit {
		return nil, "", fmt.Errorf("device limit reached (%d)", s.limit)
	}
	if !s.allowEnrollLocked(ip) {
		return nil, "", fmt.Errorf("too many enrollments from %s", ip)
	}

	id, err := s.newIDLocked()
	if err != nil {
		return nil, "", err
	}
	secret := make([]byte, 16)
	if _, err := s.randRead(secret); err != nil {
		return nil, "", err
	}
	token := deviceTokenPrefix + id + "_" + hex.EncodeToString(secret)

	now := s.now()
	d := &Device{
		ID:       id,
		Hash:     hashToken(token),
		Label:    cleanLabel(label),
		Platform: cleanLabel(platform),
		IP:       ip,
		First:    now,
		Last:     now,
	}
	s.devices[id] = d
	s.order = append(s.order, id)
	if err := s.saveLocked(); err != nil {
		delete(s.devices, id)
		s.order = s.order[:len(s.order)-1]
		return nil, "", err
	}
	log.Printf("[dt] device enrolled id=%s label=%q platform=%s ip=%s", d.ID, d.Label, d.Platform, ip)
	return copyDevice(d), token, nil
}

func (s *DeviceStore) newIDLocked() (string, error) {
	for i := 0; i < 8; i++ {
		b := make([]byte, 4)
		if _, err := s.randRead(b); err != nil {
			return "", err
		}
		id := hex.EncodeToString(b)
		if _, taken := s.devices[id]; !taken {
			return id, nil
		}
	}
	return "", fmt.Errorf("could not allocate a device id")
}

// allowEnrollLocked rate-limits enrollments per IP address.
func (s *DeviceStore) allowEnrollLocked(ip string) bool {
	if s.perHour <= 0 || ip == "" {
		return true
	}
	cut := s.now().Add(-time.Hour)
	kept := s.enrollLog[ip][:0]
	for _, t := range s.enrollLog[ip] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= s.perHour {
		s.enrollLog[ip] = kept
		return false
	}
	s.enrollLog[ip] = append(kept, s.now())
	return true
}

// ------------------------------------------------------------------ checking

// Authenticate reports whether token belongs to a live device, and records
// the connection for the admin panel.
func (s *DeviceStore) Authenticate(token, ip string) (*Device, bool) {
	if !strings.HasPrefix(token, deviceTokenPrefix) {
		return nil, false
	}
	rest := token[len(deviceTokenPrefix):]
	i := strings.IndexByte(rest, '_')
	if i <= 0 {
		return nil, false
	}
	id := rest[:i]

	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.devices[id]
	if d == nil || d.Revoked {
		return nil, false
	}
	// constant-time compare of the hash, so a wrong secret leaks nothing
	if subtle.ConstantTimeCompare([]byte(hashToken(token)), []byte(d.Hash)) != 1 {
		return nil, false
	}
	now := s.now()
	d.Last = now
	if ip != "" {
		d.IP = ip
	}
	if now.Sub(s.lastSave) > time.Minute {
		_ = s.saveLocked()
	}
	return copyDevice(d), true
}

// ------------------------------------------------------------------ managing

// SetRevoked revokes or restores a device token.
func (s *DeviceStore) SetRevoked(id string, revoked bool) (*Device, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.devices[id]
	if d == nil {
		return nil, false
	}
	d.Revoked = revoked
	d.Last = s.now()
	_ = s.saveLocked()
	return copyDevice(d), true
}

// Get returns one device by id.
func (s *DeviceStore) Get(id string) (*Device, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.devices[id]
	if d == nil {
		return nil, false
	}
	return copyDevice(d), true
}

// List returns every device, most recently seen first.
func (s *DeviceStore) List() []*Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Device, 0, len(s.devices))
	for _, d := range s.devices {
		out = append(out, copyDevice(d))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last.After(out[j].Last) })
	return out
}

// Count reports how many devices have been issued a token.
func (s *DeviceStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.devices)
}

// ---------------------------------------------------------------- small help

func copyDevice(d *Device) *Device {
	c := *d
	return &c
}

// cleanLabel keeps a client-supplied device name short and printable.
func cleanLabel(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			b.WriteRune(' ')
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
		if b.Len() >= 40 {
			break
		}
	}
	return strings.TrimSpace(b.String())
}
