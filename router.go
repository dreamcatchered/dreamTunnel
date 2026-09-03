package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// readSNI extracts server_name from a TLS ClientHello by sniffing raw bytes.
func readSNI(conn net.Conn) (string, error) {
	var hdr [5]byte
	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return "", err
	}
	if hdr[0] != 0x16 {
		return "", fmt.Errorf("not TLS")
	}
	n := int(hdr[3])<<8 | int(hdr[4])
	if n == 0 || n > 16384*2 {
		return "", fmt.Errorf("bad record length")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(conn, body); err != nil {
		return "", err
	}
	return parseSNI(body)
}

// parseSNI scans a ClientHello for the SNI extension.
func parseSNI(ch []byte) (string, error) {
	if len(ch) < 38 {
		return "", fmt.Errorf("short clienthello")
	}
	if ch[0] != 0x01 {
		return "", fmt.Errorf("not clienthello")
	}
	p := 4 + 2 + 32
	if p >= len(ch) {
		return "", fmt.Errorf("truncated")
	}
	sidLen := int(ch[p])
	p += 1 + sidLen
	if p+2 > len(ch) {
		return "", fmt.Errorf("truncated")
	}
	csLen := int(ch[p])<<8 | int(ch[p+1])
	p += 2 + csLen
	if p+1 > len(ch) {
		return "", fmt.Errorf("truncated")
	}
	compLen := int(ch[p])
	p += 1 + compLen
	if p+2 > len(ch) {
		return "", fmt.Errorf("truncated")
	}
	extLen := int(ch[p])<<8 | int(ch[p+1])
	p += 2
	extEnd := p + extLen
	if extEnd > len(ch) {
		extEnd = len(ch)
	}
	for p+4 <= extEnd {
		et := int(ch[p])<<8 | int(ch[p+1])
		el := int(ch[p+2])<<8 | int(ch[p+3])
		p += 4
		if p+el > extEnd {
			break
		}
		if et == 0 {
			d := ch[p : p+el]
			if len(d) >= 5 && d[2] == 0 {
				nameLen := int(d[3])<<8 | int(d[4])
				if 5+nameLen <= len(d) {
					return string(d[5 : 5+nameLen]), nil
				}
			}
		}
		p += el
	}
	return "", fmt.Errorf("no sni")
}

// teeConn records everything read while sniffing.
type teeConn struct {
	net.Conn
	buf bytes.Buffer
}

func (t *teeConn) Read(p []byte) (int, error) {
	n, err := t.Conn.Read(p)
	if n > 0 {
		t.buf.Write(p[:n])
	}
	return n, err
}

// prefixConn replays sniffed bytes first, then reads from the wrapped conn.
type prefixConn struct {
	net.Conn
	prefix []byte
}

func (p *prefixConn) Read(b []byte) (int, error) {
	if len(p.prefix) > 0 {
		n := copy(b, p.prefix)
		p.prefix = p.prefix[n:]
		return n, nil
	}
	return p.Conn.Read(b)
}

// sniffedConn returns a conn that re-injects the sniffed ClientHello bytes.
func sniffedConn(c net.Conn, tee *teeConn) net.Conn {
	return &prefixConn{Conn: c, prefix: tee.buf.Bytes()}
}

// EdgeListener routes connections arriving from nginx (stream module) on
// :8443: SNI sniff -> tunnel host ? proxy : static upstream.
type EdgeRouter struct {
	srv        *TunnelServer
	staticTLS  string // e.g. "127.0.0.1:9443" (nginx TLS for static hosts)
}

func (r *EdgeRouter) Serve(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		go r.Route(conn)
	}
}

// Route sniffs SNI and dispatches. Ownership of the accepted conn is
// transferred to the tunnel stream (or the static proxy); neither path
// may close it here.
func (r *EdgeRouter) Route(conn net.Conn) {
	tee := &teeConn{Conn: conn}
	sni, err := readSNI(tee)
	sni = strings.ToLower(strings.TrimSpace(sni))
	if err != nil || sni == "" {
		conn.Close()
		return
	}
	c := sniffedConn(conn, tee)

	if isTunnelHost(sni) {
		if !r.srv.Filter.CheckHost(sni) {
			c.Close() // drop silently
			return
		}
		cc := r.srv.Route(sni)
		if cc == nil {
			// tunnel offline: complete TLS then send 503 over it
			r.respondOffline(c)
			return
		}
		if cc.mode == "http" && wildTLS != nil {
			// Terminate TLS at the edge with the wildcard cert, then stream
			// plain HTTP to the client's local service. This is what makes
			// `dt http 8080` work with ordinary local HTTP servers.
			tc := tls.Server(c, wildTLS)
			tc.SetDeadline(time.Now().Add(15 * time.Second))
			if err := tc.Handshake(); err != nil {
				tc.Close()
				return
			}
			tc.SetDeadline(time.Time{})
			cc.StartStream(tc, sni)
			return
		}
		cc.StartStream(c, sni)
		return
	}

	// static host -> nginx TLS upstream
	up, err := net.DialTimeout("tcp", r.staticTLS, 5*time.Second)
	if err != nil {
		c.Close()
		return
	}
	proxyBoth(c, up)
}

// respondOffline terminates TLS with the wildcard cert and answers 503.
func (r *EdgeRouter) respondOffline(c net.Conn) {
	if wildTLS != nil {
		tc := tls.Server(c, wildTLS)
		tc.SetDeadline(time.Now().Add(10 * time.Second))
		if err := tc.Handshake(); err == nil {
			io.WriteString(tc, "HTTP/1.1 503 Service Unavailable\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: 21\r\nConnection: close\r\n\r\ntunnel is offline\r\n")
		}
	}
}

// wildTLS is the shared wildcard cert config for offline responses.
var wildTLS *tls.Config

func proxyBoth(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { io.Copy(b, a); b.Close(); a.Close(); done <- struct{}{} }()
	go func() { io.Copy(a, b); a.Close(); b.Close(); done <- struct{}{} }()
	<-done
	<-done
}

// isTunnelHost reports whether host is a dynamic tunnel label.
func isTunnelHost(host string) bool {
	return strings.HasSuffix(host, "."+TunnelDomain)
}

// TunnelDomain is the parent domain for tunnel subdomains (set by server config).
var TunnelDomain = "maiuni.online"
