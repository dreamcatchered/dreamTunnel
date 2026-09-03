package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"
)

// tlsLoadWildcard loads the wildcard cert from an LE-style dir.
func tlsLoadWildcard(dir string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(dir+"/fullchain.pem", dir+"/privkey.pem")
	if err != nil {
		return nil, fmt.Errorf("load wildcard cert: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// dialControlTLS is the client side: TLS dial with a control SNI.
// The server presents the tunnel wildcard cert (*.<domain>), so we dial
// with SNI "<label>.<domain>" derived from the tunnel domain, never the
// bare control address (which may be an IP or a static hostname).
func dialControlTLS(addr, host string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 15 * time.Second}
	sni := controlSNI(host)
	return tls.DialWithDialer(d, "tcp", addr, &tls.Config{
		ServerName: sni,
		MinVersion: tls.VersionTLS12,
	})
}

// controlSNI maps any control host to a hostname covered by the wildcard cert.
func controlSNI(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	if ip := net.ParseIP(h); ip != nil {
		// dialing by IP: use the apex tunnel domain
		return strings.TrimPrefix(TunnelDomain, "*.")
	}
	// hostname: if it is inside the tunnel domain, keep; otherwise use
	// the first label + tunnel domain so the wildcard cert matches.
	d := strings.TrimPrefix(TunnelDomain, "*.")
	if strings.HasSuffix(h, "."+d) || h == d {
		return h
	}
	labels := strings.Split(h, ".")
	if len(labels) > 0 && labels[0] != "" {
		return labels[0] + "." + d
	}
	return d
}
