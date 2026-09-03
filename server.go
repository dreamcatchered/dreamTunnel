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
}

// WelcomeMsg is the server's reply.
type WelcomeMsg struct {
	Version   int    `json:"v"`
	Host      string `json:"host"`
	Mode      string `json:"mode"`
	Reconnect bool   `json:"reconnect"`
	Filter    bool   `json:"filter"`
}

// TunnelServer hosts control connections and host -> conn routing.
type TunnelServer struct {
	mu      sync.Mutex
	byHost  map[string]*ClientConn
	bySess  map[string]*ClientConn
	counter uint32

	Secret      string
	Filter      *ContentFilter
	IdleTimeout time.Duration
}

// ClientConn wraps one authenticated control connection.
type ClientConn struct {
	srv      *TunnelServer
	host     string
	sess     string
	mode     string
	port     int
	wmu      sync.Mutex
	conn     net.Conn
	streams  map[uint32]*PublicStream
	smu      sync.Mutex
	lastSeen time.Time
	closed   bool
}

// PublicStream is one end-user connection being proxied.
type PublicStream struct {
	id     uint32
	client *ClientConn
	conn   net.Conn
}

func NewTunnelServer(secret string, filter *ContentFilter, idle time.Duration) *TunnelServer {
	return &TunnelServer{
		byHost:      map[string]*ClientConn{},
		bySess:      map[string]*ClientConn{},
		Secret:      secret,
		Filter:      filter,
		IdleTimeout: idle,
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
	cc.smu.Lock()
	cc.streams[id] = st
	cc.smu.Unlock()

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
	if !serveAuth(token, s.Secret) {
		WriteFrame(conn, PktDenied, []byte("auth failed"))
		log.Printf("[dt] auth failed sess=%s", hello.Sess)
		return
	}

	if !s.Filter.CheckHost(hello.Subdomain) {
		WriteFrame(conn, PktDenied, []byte("subdomain rejected"))
		return
	}

	cc := &ClientConn{
		srv:      s,
		sess:     hello.Sess,
		mode:     hello.Mode,
		port:     hello.LocalPort,
		conn:     conn,
		streams:  map[uint32]*PublicStream{},
		lastSeen: time.Now(),
	}

	host, resumed, err := s.Register(cc, hello.Subdomain)
	if err != nil {
		WriteFrame(conn, PktDenied, []byte(err.Error()))
		return
	}

	wel := WelcomeMsg{Version: 1, Host: host, Mode: cc.mode, Reconnect: resumed, Filter: s.Filter.Enabled}
	wb, _ := json.Marshal(wel)
	if err := WriteFrame(conn, PktWelcome, wb); err != nil {
		s.Unregister(cc)
		return
	}
	conn.SetDeadline(time.Time{})

	log.Printf("[dt] client %s host=%s mode=%s resumed=%v", platformTag(), host, cc.mode, resumed)
	cc.HandleControl()
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
