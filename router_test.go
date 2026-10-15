package main

import (
	"bytes"
	"io"
	"net"
	"testing"
)

func readAll(t *testing.T, c net.Conn, n int) []byte {
	t.Helper()
	got := make([]byte, n)
	if _, err := io.ReadFull(c, got); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	return got
}

// nginx stream sends "PROXY TCP4 ...\r\n" before the ClientHello; the edge
// must strip it and keep the TLS bytes intact.
func TestStripProxyV1(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	payload := []byte{0x16, 0x03, 0x01, 0x00, 0x2f, 0x01, 0x00, 0x00, 0x2d, 0x03, 0x03}
	go func() {
		c2.Write([]byte("PROXY TCP4 203.0.113.7 10.0.0.1 5555 443\r\n"))
		c2.Write(payload)
	}()

	conn, ip, err := stripProxy(c1)
	if err != nil {
		t.Fatalf("stripProxy: %v", err)
	}
	if ip != "203.0.113.7" {
		t.Fatalf("source ip = %q, want 203.0.113.7", ip)
	}
	if got := readAll(t, conn, len(payload)); !bytes.Equal(got, payload) {
		t.Fatalf("payload = %x, want %x", got, payload)
	}
}

// A direct connection has no header and must be passed through unchanged.
func TestStripProxyAbsent(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	payload := []byte{0x16, 0x03, 0x01, 0x00, 0x2f, 0x01, 0x00, 0x00}
	go func() { c2.Write(payload) }()

	conn, ip, err := stripProxy(c1)
	if err != nil {
		t.Fatalf("stripProxy: %v", err)
	}
	if ip != "" {
		t.Fatalf("unexpected source ip %q", ip)
	}
	if got := readAll(t, conn, len(payload)); !bytes.Equal(got, payload) {
		t.Fatalf("payload = %x, want %x", got, payload)
	}
}

// PROXY v2 (binary) must be stripped as well.
func TestStripProxyV2(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	payload := []byte{0x16, 0x03, 0x01, 0x00, 0x2f}
	hdr := append([]byte(nil), proxyV2Sig...)
	hdr = append(hdr, 0x21, 0x11, 0x00, 0x0c) // v2, PROXY, STREAM, IPv4, len=12
	hdr = append(hdr, 198, 51, 100, 9)        // src
	hdr = append(hdr, 10, 0, 0, 1)            // dst
	hdr = append(hdr, 0x04, 0xd2, 0x01, 0xbb) // ports
	go func() {
		c2.Write(hdr)
		c2.Write(payload)
	}()

	conn, ip, err := stripProxy(c1)
	if err != nil {
		t.Fatalf("stripProxy: %v", err)
	}
	if ip != "198.51.100.9" {
		t.Fatalf("source ip = %q, want 198.51.100.9", ip)
	}
	if got := readAll(t, conn, len(payload)); !bytes.Equal(got, payload) {
		t.Fatalf("payload = %x, want %x", got, payload)
	}
}

// Short TLS records must not be mistaken for a PROXY header.
func TestStripProxyShortRecord(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	payload := []byte{0x16, 0x03, 0x01}
	go func() { c2.Write(payload) }()

	conn, _, err := stripProxy(c1)
	if err != nil {
		t.Fatalf("stripProxy: %v", err)
	}
	if got := readAll(t, conn, len(payload)); !bytes.Equal(got, payload) {
		t.Fatalf("payload = %x, want %x", got, payload)
	}
}
