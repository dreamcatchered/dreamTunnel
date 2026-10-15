package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *DeviceStore {
	t.Helper()
	s, err := LoadDeviceStore(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return s
}

func TestDeviceStoreEnrollAndAuthenticate(t *testing.T) {
	s := newTestStore(t)
	dev, token, err := s.Enroll("peter-pc", "windows/amd64", "203.0.113.7")
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if !strings.HasPrefix(token, deviceTokenPrefix+dev.ID+"_") {
		t.Fatalf("token %q does not carry the device id %q", token, dev.ID)
	}
	if len(dev.ID) != 8 {
		t.Fatalf("device id should be 8 hex chars, got %q", dev.ID)
	}
	if dev.Label != "peter-pc" || dev.Platform != "windows/amd64" {
		t.Fatalf("device metadata not recorded: %+v", dev)
	}

	got, ok := s.Authenticate(token, "198.51.100.9")
	if !ok {
		t.Fatal("a freshly issued token must authenticate")
	}
	if got.ID != dev.ID {
		t.Fatalf("authenticated as %q, want %q", got.ID, dev.ID)
	}
	// the connection is recorded for the admin panel
	if got.IP != "198.51.100.9" {
		t.Fatalf("last ip not updated: %q", got.IP)
	}

	if _, ok := s.Authenticate("dtd_deadbeef_00000000000000000000000000000000", ""); ok {
		t.Fatal("a token for an unknown device must be rejected")
	}
	if _, ok := s.Authenticate("garbage", ""); ok {
		t.Fatal("a token that is not a device token must be rejected")
	}
	// right id, wrong secret
	wrong := deviceTokenPrefix + dev.ID + "_00000000000000000000000000000000"
	if _, ok := s.Authenticate(wrong, ""); ok {
		t.Fatal("a forged secret must be rejected")
	}
}

func TestDeviceStoreRevoke(t *testing.T) {
	s := newTestStore(t)
	dev, token, err := s.Enroll("phone", "android/arm64", "203.0.113.8")
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if _, ok := s.SetRevoked(dev.ID, true); !ok {
		t.Fatal("revoke should find the device")
	}
	if _, ok := s.Authenticate(token, ""); ok {
		t.Fatal("a revoked device must not authenticate")
	}
	if _, ok := s.SetRevoked(dev.ID, false); !ok {
		t.Fatal("restore should find the device")
	}
	if _, ok := s.Authenticate(token, ""); !ok {
		t.Fatal("a restored device must authenticate again")
	}
	if _, ok := s.SetRevoked("nope", true); ok {
		t.Fatal("revoking an unknown id must fail")
	}
}

func TestDeviceStorePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	s, err := LoadDeviceStore(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	dev, token, err := s.Enroll("laptop", "darwin/arm64", "203.0.113.9")
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}

	again, err := LoadDeviceStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if again.Count() != 1 {
		t.Fatalf("expected 1 device after reload, got %d", again.Count())
	}
	got, ok := again.Authenticate(token, "")
	if !ok || got.ID != dev.ID {
		t.Fatalf("token did not survive a reload (ok=%v id=%q)", ok, got.ID)
	}
}

// The store must never hold a usable token: only its hash.
func TestDeviceStoreKeepsNoPlaintextToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	s, err := LoadDeviceStore(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	_, token, err := s.Enroll("box", "linux/amd64", "203.0.113.10")
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store: %v", err)
	}
	if strings.Contains(string(data), token) {
		t.Fatal("the device token must not be written to disk in the clear")
	}
	if !strings.Contains(string(data), hashToken(token)) {
		t.Fatal("the token hash should be stored")
	}
	if fi, err := os.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	} else if fi.Mode().Perm() != 0600 {
		t.Fatalf("store should be 0600, got %v", fi.Mode().Perm())
	}
}

func TestDeviceStoreRateLimit(t *testing.T) {
	s := newTestStore(t)
	s.perHour = 2
	for i := 0; i < 2; i++ {
		if _, _, err := s.Enroll("d", "linux/amd64", "203.0.113.11"); err != nil {
			t.Fatalf("enroll %d: %v", i, err)
		}
	}
	if _, _, err := s.Enroll("d", "linux/amd64", "203.0.113.11"); err == nil {
		t.Fatal("a third enrollment from one ip within the hour must be refused")
	}
	// a different ip is unaffected
	if _, _, err := s.Enroll("d", "linux/amd64", "203.0.113.12"); err != nil {
		t.Fatalf("another ip should still enroll: %v", err)
	}
}

func TestDeviceStoreLimit(t *testing.T) {
	s := newTestStore(t)
	s.limit = 1
	s.perHour = 0
	if _, _, err := s.Enroll("a", "linux/amd64", "10.0.0.1"); err != nil {
		t.Fatalf("first enroll: %v", err)
	}
	if _, _, err := s.Enroll("b", "linux/amd64", "10.0.0.2"); err == nil {
		t.Fatal("the total device limit must be enforced")
	}
}

func TestDeviceStoreListNewestFirst(t *testing.T) {
	s := newTestStore(t)
	first, _, err := s.Enroll("old", "linux/amd64", "10.0.0.1")
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	second, _, err := s.Enroll("new", "linux/amd64", "10.0.0.2")
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	list := s.List()
	if len(list) != 2 || list[0].ID != second.ID || list[1].ID != first.ID {
		t.Fatalf("list should be newest-first, got %+v", list)
	}
}

// classifyAuth must keep the master secret, device tokens and the shared
// enrollment token apart.
func TestClassifyAuth(t *testing.T) {
	store := newTestStore(t)
	srv := NewTunnelServer(ServerOptions{
		Secret:      "master-secret",
		EnrollToken: "shared-token",
		Devices:     store,
		Filter:      NewContentFilter(false),
	})

	if got := srv.classifyAuth("master-secret", "10.0.0.1"); got.kind != "master" {
		t.Fatalf("master secret classified as %q", got.kind)
	}
	if got := srv.classifyAuth("shared-token", "10.0.0.1"); got.kind != "enroll" {
		t.Fatalf("shared token classified as %q", got.kind)
	}
	if got := srv.classifyAuth("nonsense", "10.0.0.1"); got.kind != "" {
		t.Fatalf("garbage classified as %q", got.kind)
	}

	dev, token, err := store.Enroll("laptop", "linux/amd64", "10.0.0.1")
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	got := srv.classifyAuth(token, "10.0.0.2")
	if got.kind != "device" || got.deviceID != dev.ID {
		t.Fatalf("device token classified as %q/%q", got.kind, got.deviceID)
	}

	// a revoked device falls through to "nothing matched"
	store.SetRevoked(dev.ID, true)
	if got := srv.classifyAuth(token, "10.0.0.2"); got.kind != "" {
		t.Fatalf("revoked device classified as %q", got.kind)
	}
}

// Enrollment can be switched off entirely (Devices == nil).
func TestClassifyAuthWithoutStore(t *testing.T) {
	srv := NewTunnelServer(ServerOptions{
		Secret:      "master-secret",
		EnrollToken: "shared-token",
		Filter:      NewContentFilter(false),
	})
	if got := srv.classifyAuth("shared-token", ""); got.kind != "enroll" {
		t.Fatalf("shared token classified as %q", got.kind)
	}
	if got := srv.classifyAuth("dtd_12345678_00", ""); got.kind != "" {
		t.Fatalf("device token without a store classified as %q", got.kind)
	}
}

func TestDeviceIDFromToken(t *testing.T) {
	plain := "dtd_7f3a1b2c_0123456789abcdef0123456789abcdef"
	id, ok := deviceIDFromToken(plain)
	if !ok || id != "7f3a1b2c" {
		t.Fatalf("plain token parsed as %q ok=%v", id, ok)
	}
	ob, err := tokenObfuscate(ObfuscationPass, plain)
	if err != nil {
		t.Fatalf("obfuscate: %v", err)
	}
	if id, ok := deviceIDFromToken(ob); !ok || id != "7f3a1b2c" {
		t.Fatalf("obfuscated token parsed as %q ok=%v", id, ok)
	}
	if _, ok := deviceIDFromToken("6829abcd"); ok {
		t.Fatal("a plain master token is not a device token")
	}
}

func TestCleanLabel(t *testing.T) {
	if got := cleanLabel("  peter-pc\n"); got != "peter-pc" {
		t.Fatalf("label trimmed to %q", got)
	}
	long := strings.Repeat("x", 100)
	if got := cleanLabel(long); len(got) > 40 {
		t.Fatalf("label not truncated: %d chars", len(got))
	}
	if got := cleanLabel("\x00\x01bad"); strings.ContainsAny(got, "\x00\x01") {
		t.Fatalf("control characters survived: %q", got)
	}
}
