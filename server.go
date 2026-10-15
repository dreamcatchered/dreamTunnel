package main

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

// HelloMsg is sent by the client right after the control TLS connect.
type HelloMsg struct {
	Version   int    `json:"v"`
	Token     string `json:"token"` // obfuscated or plain
	Subdomain string `json:"sub"`
	Mode      string `json:"mode"`
	LocalPort int    `json:"port"`
	Sess      string `json:"sess"`

	// Enroll asks the server for a token of this device's own. It is only
	// honoured when the presented token is the shared enrollment token.
	Enroll bool `json:"enroll,omitempty"`
	// Label and Platform describe the device for the admin panel.
	Label    string `json:"label,omitempty"`
	Platform string `json:"platform,omitempty"`
}

// WelcomeMsg is the server's reply.
type WelcomeMsg struct {
	Version   int    `json:"v"`
	Host      string `json:"host"`
	Mode      string `json:"mode"`
	Reconnect bool   `json:"reconnect"`
	Filter    bool   `json:"filter"`

	// Issued carries a freshly issued device token; the client stores it and
	// uses it from then on. DeviceID identifies the device on the server.
	Issued   string `json:"issued,omitempty"`
	DeviceID string `json:"device,omitempty"`
}

// TunnelServer hosts control connections and host -> conn routing.
type TunnelServer struct {
	mu      sync.Mutex
	byHost  map[string]*ClientConn
	bySess  map[string]*ClientConn
	counter uint32

	Secret string
	// EnrollToken is the shared token compiled into official builds. It is a
	// bootstrapping credential: it authorises a device to ask for a token of
	// its own, and (unless RequireDevice is set) still opens a tunnel by
	// itself for clients that have not enrolled yet.
	EnrollToken string
	// RequireDevice refuses the shared token as a tunnel credential, so only
	// per-device tokens and the master secret work.
	RequireDevice bool
	// Devices stores the issued per-device tokens. Nil disables enrollment.
	Devices     *DeviceStore
	Filter      *ContentFilter
	IdleTimeout time.Duration

	// admin state
	adminMu   sync.Mutex
	history   []HistoryEntry // ring of past sessions
	bannedIP  map[string]time.Time
	rateAuth  map[string]*rateBucket // failed auth attempts per IP
	stats     ServerStats
}

// HistoryEntry is a completed or active client session record.
type HistoryEntry struct {
	Sess    string    `json:"sess"`
	Host    string    `json:"host"`
	Mode    string    `json:"mode"`
	Port    int       `json:"port"`
	IP      string    `json:"ip"`
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
	Streams int64     `json:"streams"`
	BytesIn int64     `json:"bytesIn"`  // public -> client
	BytesOut int64    `json:"bytesOut"` // client -> public
	Active  bool      `json:"active"`
	Killed  bool      `json:"killed,omitempty"`
}

// ServerStats aggregates counters for the admin panel.
type ServerStats struct {
	TotalSessions int64 `json:"totalSessions"`
	TotalStreams  int64 `json:"totalStreams"`
	TotalBytesIn  int64 `json:"totalBytesIn"`
	TotalBytesOut int64 `json:"totalBytesOut"`
	AuthFails     int64 `json:"authFails"`
}

// rateBucket is a token-bucket style limiter for brute-force protection.
type rateBucket struct {
	hits    int
	blocked time.Time
}

const (
	historyLimit = 500
	maxAuthFails = 5
	authBanTime  = 15 * time.Minute
)

// ClientConn wraps one authenticated control connection.
type ClientConn struct {
	srv      *TunnelServer
	host     string
	sess     string
	mode     string
	port     int
	ip       string
	device   string // per-device token id, empty for master/shared tokens
	wmu      sync.Mutex
	conn     net.Conn
	streams  map[uint32]*PublicStream
	smu      sync.Mutex
	lastSeen time.Time
	started  time.Time
	closed   bool

	// per-connection stats (atomic-ish under smu)
	statsMu   sync.Mutex
	streamsN  int64
	bytesIn   int64
	bytesOut  int64
}

// PublicStream is one end-user connection being proxied.
type PublicStream struct {
	id     uint32
	client *ClientConn
	conn   net.Conn
}

// ServerOptions configures a TunnelServer.
type ServerOptions struct {
	Secret        string
	EnrollToken   string
	RequireDevice bool
	Devices       *DeviceStore
	Filter        *ContentFilter
	IdleTimeout   time.Duration
}

func NewTunnelServer(o ServerOptions) *TunnelServer {
	return &TunnelServer{
		byHost:        map[string]*ClientConn{},
		bySess:        map[string]*ClientConn{},
		Secret:        o.Secret,
		EnrollToken:   o.EnrollToken,
		RequireDevice: o.RequireDevice,
		Devices:       o.Devices,
		Filter:        o.Filter,
		IdleTimeout:   o.IdleTimeout,
		history:       []HistoryEntry{},
		bannedIP:      map[string]time.Time{},
		rateAuth:      map[string]*rateBucket{},
	}
}

func (s *TunnelServer) nextID() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counter++
	if s.counter == 0 {
		s.counter = 1
	}
	return s.counter
}

// randLabel generates a short random subdomain label like "k7f3m9ab".
func randLabel() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Register maps cc to a host, resuming cc.sess when possible. Returned host
// is the FQDN (label + "." + TunnelDomain); byHost is keyed by FQDN.
func (s *TunnelServer) Register(cc *ClientConn, requested string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fqdn := func(label string) string {
		if TunnelDomain != "" && !strings.HasSuffix(label, "."+TunnelDomain) {
			return label + "." + TunnelDomain
		}
		return label
	}

	// Resume: same session reconnects to its old host.
	if cc.sess != "" {
		if old, ok := s.bySess[cc.sess]; ok && old != cc {
			old.closed = true
			old.conn.Close()
		}
		if oldLabel, ok := sessHost(cc.sess); ok {
			oldHost := fqdn(oldLabel)
			if cur, ok2 := s.byHost[oldHost]; !ok2 || cur == cc {
				cc.host = oldHost
				s.byHost[oldHost] = cc
				s.bySess[cc.sess] = cc
				return oldHost, true, nil
			}
		}
	}

	base := safeHost(requested)
	if base == "" {
		base = randLabel()
	}

	// If another LIVE client holds the requested label, take it over
	// when its control conn is dead, otherwise pick a free variant
	// (name2, name3, ... then random). Two users never share a host.
	if existing, ok := s.byHost[fqdn(base)]; ok && existing != cc {
		if !existing.isAlive() {
			// stale registration: reclaim the label
			existing.closed = true
			existing.conn.Close()
			delete(s.byHost, fqdn(base))
		}
	}
	label := base
	for i := 0; i < 8; i++ {
		if existing, ok := s.byHost[fqdn(label)]; !ok || existing == cc {
			break
		}
		label = fmt.Sprintf("%s%d", base, i+2)
		if i == 7 {
			label = randLabel()
		}
	}
	host := fqdn(label)
	if existing, ok := s.byHost[host]; ok && existing != cc {
		return "", false, fmt.Errorf("host busy: %s", host)
	}
	cc.host = host
	s.byHost[host] = cc
	if cc.sess != "" {
		s.bySess[cc.sess] = cc
		rememberSessHost(cc.sess, label)
	}
	return host, false, nil
}

// sessHost remembers sess -> host assignments in memory.
var (
	sessMu    sync.Mutex
	sessHosts = map[string]string{}
)

func sessHost(sess string) (string, bool) {
	sessMu.Lock()
	defer sessMu.Unlock()
	h, ok := sessHosts[sess]
	return h, ok
}

func rememberSessHost(sess, host string) {
	sessMu.Lock()
	defer sessMu.Unlock()
	sessHosts[sess] = host
}

// Unregister removes a client's mappings.

// isAlive reports whether the control connection is still usable.
// A stale registration (dead conn, last seen long ago) can be reclaimed.
func (cc *ClientConn) isAlive() bool {
	if cc.closed || cc.conn == nil {
		return false
	}
	// the ping loop refreshes lastSeen; anything older is a zombie
	return time.Since(cc.lastSeen) < 90*time.Second
}
func (s *TunnelServer) Unregister(cc *ClientConn) {
	s.mu.Lock()
	if cc.host != "" {
		if cur, ok := s.byHost[cc.host]; ok && cur == cc {
			delete(s.byHost, cc.host)
		}
	}
	if cc.sess != "" {
		if cur, ok := s.bySess[cc.sess]; ok && cur == cc {
			delete(s.bySess, cc.sess)
		}
	}
	s.mu.Unlock()
}

// Route finds the live client conn owning a host.
func (s *TunnelServer) Route(host string) *ClientConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	cc := s.byHost[host]
	if cc == nil || cc.closed {
		return nil
	}
	return cc
}

// writeFrame serializes control-conn writes.
func (cc *ClientConn) writeFrame(typ byte, payload []byte) error {
	cc.wmu.Lock()
	defer cc.wmu.Unlock()
	if cc.closed {
		return fmt.Errorf("conn closed")
	}
	return WriteFrame(cc.conn, typ, payload)
}

// HandleControl runs the client control loop after a successful Hello/Welcome.
func (cc *ClientConn) HandleControl() {
	defer func() {
		cc.closed = true
		cc.srv.Unregister(cc)
		cc.closeStreams()
		cc.conn.Close()
	}()

	// server ping loop
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for range t.C {
			if cc.closed {
				return
			}
			if err := cc.writeFrame(PktPing, nil); err != nil {
				cc.conn.Close()
				return
			}
		}
	}()

	for {
		cc.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		typ, payload, err := ReadFrame(cc.conn)
		if err != nil {
			cc.srv.finishSession(cc, false)
			return
		}
		cc.lastSeen = time.Now()

		switch typ {
		case PktPong:
		case PktPing:
			cc.writeFrame(PktPong, nil)
		case PktData:
			if len(payload) < 4 {
				continue
			}
			id := GetU32(payload[:4])
			cc.statsMu.Lock()
			cc.bytesOut += int64(len(payload) - 4)
			cc.statsMu.Unlock()
			st := cc.getStream(id)
			if st != nil {
				st.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
				if _, err := st.conn.Write(payload[4:]); err != nil {
					cc.closeStream(id)
					cc.writeFrame(PktClose, u32b(id))
				}
			}
		case PktClose:
			if len(payload) >= 4 {
				cc.closeStream(GetU32(payload[:4]))
			}
		default:
			// ignore unknown
		}
	}
}

// StartStream registers a public conn, asks the client to dial its local
// service, and pumps public->tunnel bytes.
func (cc *ClientConn) StartStream(pc net.Conn, hostHint string) error {
	id := cc.srv.nextID()
	st := &PublicStream{id: id, client: cc, conn: pc}
	remote := "?"
	if ra := pc.RemoteAddr(); ra != nil {
		remote = ra.String()
	}
	log.Printf("[dt] conn %s -> %s stream=%d", remote, cc.host, id)
	cc.smu.Lock()
	cc.streams[id] = st
	cc.smu.Unlock()
	cc.statsMu.Lock()
	cc.streamsN++
	cc.statsMu.Unlock()

	payload := append(u32b(id), []byte(hostHint)...)
	if err := cc.writeFrame(PktOpen, payload); err != nil {
		cc.smu.Lock()
		delete(cc.streams, id)
		cc.smu.Unlock()
		pc.Close()
		return err
	}

	go func() {
		buf := make([]byte, 32*1024)
		for {
			pc.SetReadDeadline(time.Now().Add(5 * time.Minute))
			n, err := pc.Read(buf)
			if n > 0 {
				cc.statsMu.Lock()
				cc.bytesIn += int64(n)
				cc.statsMu.Unlock()
				frame := append(u32b(id), buf[:n]...)
				if werr := cc.writeFrame(PktData, frame); werr != nil {
					cc.closeStream(id)
					pc.Close()
					return
				}
			}
			if err != nil {
				cc.closeStream(id)
				cc.writeFrame(PktClose, u32b(id))
				pc.Close()
				return
			}
		}
	}()
	return nil
}

func (cc *ClientConn) getStream(id uint32) *PublicStream {
	cc.smu.Lock()
	defer cc.smu.Unlock()
	return cc.streams[id]
}

func (cc *ClientConn) closeStream(id uint32) {
	cc.smu.Lock()
	st := cc.streams[id]
	delete(cc.streams, id)
	cc.smu.Unlock()
	if st != nil {
		st.conn.Close()
	}
}

func (cc *ClientConn) closeStreams() {
	cc.smu.Lock()
	streams := cc.streams
	cc.streams = map[uint32]*PublicStream{}
	cc.smu.Unlock()
	for _, st := range streams {
		st.conn.Close()
	}
}

// ServeControlConn performs Hello/Welcome auth on a fresh TLS conn.
func (s *TunnelServer) ServeControlConn(conn net.Conn) {
	defer conn.Close()
	ip := "?"
	if ra := conn.RemoteAddr(); ra != nil {
		ip = hostOnly(ra.String())
	}

	// banned IP check
	if s.IsIPBanned(ip) {
		log.Printf("[dt] rejected banned ip %s", ip)
		return
	}
	// brute-force lockout check
	if s.authBlocked(ip) {
		WriteFrame(conn, PktDenied, []byte("rate limited"))
		return
	}

	conn.SetDeadline(time.Now().Add(20 * time.Second))
	typ, payload, err := ReadFrame(conn)
	if err != nil || typ != PktHello {
		return
	}
	var hello HelloMsg
	if err := json.Unmarshal(payload, &hello); err != nil {
		WriteFrame(conn, PktDenied, []byte("bad hello"))
		return
	}

	token := hello.Token
	if len(token) > 8 && strings.HasPrefix(strings.ToLower(token[:2]), "x") == false && isHex(token) {
		if de, err := tokenDeobfuscate(ObfuscationPass, token); err == nil {
			token = de
		}
	}
	authKind := ""
	deviceID := ""
	issued := ""
	if dec := s.classifyAuth(token, ip); dec.kind != "" {
		authKind, deviceID = dec.kind, dec.deviceID
	}
	if authKind == "enroll" {
		// The shared token is only a bootstrapping credential. It still opens
		// a tunnel by itself (that is how clients that have never enrolled
		// connect) unless the operator requires device tokens. With the Enroll
		// flag set the server mints a token for this device instead.
		switch {
		case !hello.Enroll && s.RequireDevice:
			WriteFrame(conn, PktDenied, []byte("device token required: run `dt enroll`"))
			log.Printf("[dt] denied shared token ip=%s (device tokens required)", ip)
			return
		case !hello.Enroll:
			authKind = "public"
		case s.Devices == nil:
			WriteFrame(conn, PktDenied, []byte("enrollment disabled"))
			return
		default:
			dev, tok, err := s.Devices.Enroll(hello.Label, hello.Platform, ip)
			if err != nil {
				WriteFrame(conn, PktDenied, []byte(err.Error()))
				log.Printf("[dt] enrollment failed ip=%s: %v", ip, err)
				return
			}
			issued, deviceID, authKind = tok, dev.ID, "device"
		}
	}
	if authKind == "" {
		WriteFrame(conn, PktDenied, []byte("auth failed"))
		s.authFail(ip)
		log.Printf("[dt] auth failed ip=%s sess=%s", ip, hello.Sess)
		return
	}
	s.authOK(ip)

	if !s.Filter.CheckHost(hello.Subdomain) {
		WriteFrame(conn, PktDenied, []byte("subdomain rejected"))
		return
	}

	cc := &ClientConn{
		srv:      s,
		sess:     hello.Sess,
		mode:     hello.Mode,
		port:     hello.LocalPort,
		ip:       ip,
		device:   deviceID,
		conn:     conn,
		streams:  map[uint32]*PublicStream{},
		lastSeen: time.Now(),
		started:  time.Now(),
	}

	host, resumed, err := s.Register(cc, hello.Subdomain)
	if err != nil {
		WriteFrame(conn, PktDenied, []byte(err.Error()))
		return
	}

	wel := WelcomeMsg{
		Version: 1, Host: host, Mode: cc.mode, Reconnect: resumed,
		Filter: s.Filter.Enabled, Issued: issued, DeviceID: deviceID,
	}
	wb, _ := json.Marshal(wel)
	if err := WriteFrame(conn, PktWelcome, wb); err != nil {
		s.Unregister(cc)
		return
	}
	conn.SetDeadline(time.Time{})

	s.recordSession(cc, resumed)

	if deviceID != "" {
		log.Printf("[dt] client %s ip=%s host=%s mode=%s resumed=%v auth=%s device=%s", platformTag(), ip, host, cc.mode, resumed, authKind, deviceID)
	} else {
		log.Printf("[dt] client %s ip=%s host=%s mode=%s resumed=%v auth=%s", platformTag(), ip, host, cc.mode, resumed, authKind)
	}
	cc.HandleControl()
}

// authDecision is what a presented token turned out to be.
type authDecision struct {
	kind     string // "master", "device", "enroll", or "" when nothing matched
	deviceID string
}

// classifyAuth decides what a presented token may do. Unlike the master
// secret, a device token is looked up in the store, so a revoked device is
// refused here.
func (s *TunnelServer) classifyAuth(token, ip string) authDecision {
	if serveAuth(token, s.Secret) {
		return authDecision{kind: "master"}
	}
	if s.Devices != nil {
		if d, ok := s.Devices.Authenticate(token, ip); ok {
			return authDecision{kind: "device", deviceID: d.ID}
		}
	}
	if s.EnrollToken != "" && serveAuth(token, s.EnrollToken) {
		return authDecision{kind: "enroll"}
	}
	return authDecision{}
}

// ServeControlTLSConn wraps a raw conn in TLS (wildcard cert) and runs the
// Hello/Welcome handshake on it.
func (s *TunnelServer) ServeControlTLSConn(conn net.Conn) {
	if wildTLS == nil {
		conn.Close()
		return
	}
	tc := tls.Server(conn, wildTLS)
	tc.SetDeadline(time.Now().Add(15 * time.Second))
	if err := tc.Handshake(); err != nil {
		tc.Close()
		return
	}
	tc.SetDeadline(time.Time{})
	s.ServeControlConn(tc)
}

// ObfuscationPass is the shared passphrase for token obfuscation.
var ObfuscationPass = "dreamtunnel.v1"

func isHex(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return len(s) > 0
}

var _ = strings.ToLower
