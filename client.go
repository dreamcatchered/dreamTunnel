package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// clientConfig is stored at ~/.dreamtunnel/config.env (override with DT_CONFIG).
type clientConfig struct {
	Server string
	Token  string
	Sub    string
	Sess   string
}

func clientConfigPath() string {
	// DT_CONFIG lets a caller keep its own config (and therefore its own
	// session id), so several tunnels can run side by side.
	if p := os.Getenv("DT_CONFIG"); p != "" {
		return p
	}
	return joinHome(".dreamtunnel", "config.env")
}

func loadClientConfig() *clientConfig {
	env := dotEnv(clientConfigPath())
	cfg := &clientConfig{
		Server: env["DT_SERVER"],
		Token:  env["DT_TOKEN"],
		Sub:    env["DT_SUB"],
		Sess:   env["DT_SESS"],
	}
	// process env overrides (handy for CI and one-off runs)
	if v := os.Getenv("DT_SERVER"); v != "" {
		cfg.Server = v
	}
	if v := os.Getenv("DT_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("DT_SUB"); v != "" {
		cfg.Sub = safeHost(v)
	}
	return cfg
}

func saveClientConfig(cfg *clientConfig) error {
	data := fmt.Sprintf("DT_SERVER=%s\nDT_TOKEN=%s\nDT_SUB=%s\nDT_SESS=%s\n",
		cfg.Server, cfg.Token, cfg.Sub, cfg.Sess)
	return writeFileAtomic(clientConfigPath(), []byte(data), 0600)
}

// ensureSession loads or creates a persistent session id.
func ensureSession(cfg *clientConfig) {
	if cfg.Sess != "" {
		return
	}
	b := make([]byte, 8)
	randRead(b)
	cfg.Sess = hexEncode(b)
	saveClientConfig(cfg)
}

// runTunnel is the entrypoint of `dt http <port>` / `dt tcp <port>`.
// Args: <port> [name] [--server host:port] — name (positional or --sub)
// may come in any order relative to flags.
func runTunnel(mode string, args []string) error {
	port := ""
	sub := ""
	server := ""
	rest := args[:0]
	for _, a := range args {
		switch {
		case a == "--server" || a == "-server":
			// handled below with lookahead
			rest = append(rest, a)
		case strings.HasPrefix(a, "--server="):
			server = strings.TrimPrefix(a, "--server=")
		case a == "--sub" || a == "-sub":
			rest = append(rest, a)
		case strings.HasPrefix(a, "--sub="):
			sub = strings.TrimPrefix(a, "--sub=")
		case a == "--json":
			jsonOut = true
		default:
			rest = append(rest, a)
		}
	}
	// two-token forms
	for i := 0; i < len(rest); i++ {
		if (rest[i] == "--server" || rest[i] == "-server") && i+1 < len(rest) {
			server = rest[i+1]
			rest = append(rest[:i], rest[i+2:]...)
			i--
		} else if (rest[i] == "--sub" || rest[i] == "-sub") && i+1 < len(rest) {
			sub = rest[i+1]
			rest = append(rest[:i], rest[i+2:]...)
			i--
		}
	}
	// positional: first is port, second (optional) is subdomain name
	pos := []string{}
	for _, a := range rest {
		if !strings.HasPrefix(a, "-") {
			pos = append(pos, a)
		}
	}
	if len(pos) > 0 {
		port = pos[0]
	}
	if len(pos) > 1 && sub == "" {
		sub = pos[1]
	}
	if sub == "" {
		sub = os.Getenv("DT_SUB")
	}

	if port == "" {
		return fmt.Errorf("usage: dt %s <local-port> [name] [--server host:port]", mode)
	}
	if _, err := net.LookupPort("tcp", port); err != nil {
		// maybe the first positional is the subdomain and second the port
		if len(pos) > 1 {
			if _, err2 := net.LookupPort("tcp", pos[1]); err2 == nil {
				port, sub = pos[1], pos[0]
			}
		}
		if _, err := net.LookupPort("tcp", port); err != nil {
			return fmt.Errorf("invalid port %q", port)
		}
	}

	cfg := loadClientConfig()
	if server != "" {
		cfg.Server = server
	}
	if sub != "" {
		cfg.Sub = safeHost(sub)
	}
	if cfg.Server == "" {
		cfg.Server = DefaultServer
	}
	// Token resolution: config file < env < compiled-in public token.
	// The public token makes a fresh install work with zero setup.
	token := cfg.Token
	if token == "" {
		token = DefaultPublicToken
		if token == "" {
			return fmt.Errorf("no token: run `dt login` first (or set DT_TOKEN)")
		}
		if !jsonOut {
			fmt.Fprintln(os.Stderr, "[dt] публичный токен сервера — настройка не требуется")
		}
	}
	ensureSession(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		fmt.Println("\n[dt] shutting down")
		cancel()
	}()

	t := &TunnelClient{
		cfg:     cfg,
		mode:    mode,
		port:    port,
		token:   token,
		streams: map[uint32]net.Conn{},
		insp:    map[uint32]*httpInspector{},
		tlog:    newTunnelLogger(),
	}
	t.SetRunCtx(ctx)
	return t.Run(ctx)
}

// DefaultServer is the public control endpoint.
const DefaultServer = "tunnel.swag.best:444"

// DefaultPublicToken is the shared token baked into official builds with
//
//	-ldflags "-X main.DefaultPublicToken=<obfuscated token>"
//
// When it is set, `dt http <port>` works right after install: there is no
// login step and no DT_TOKEN to configure. Source builds leave it empty and
// keep the old "run dt login first" behaviour.
var DefaultPublicToken = ""

// jsonOut switches stdout to machine-readable NDJSON events (--json).
var jsonOut bool

var emitMu sync.Mutex

// emitEvent writes one NDJSON line to stdout when --json is active.
// Every event carries "event"; extra fields are merged in.
func emitEvent(kind string, fields map[string]interface{}) {
	if !jsonOut {
		return
	}
	if fields == nil {
		fields = map[string]interface{}{}
	}
	fields["event"] = kind
	b, err := json.Marshal(fields)
	if err != nil {
		return
	}
	emitMu.Lock()
	defer emitMu.Unlock()
	os.Stdout.Write(append(b, '\n'))
}

// TunnelClient maintains the control connection with auto-reconnect.
type TunnelClient struct {
	cfg     *clientConfig
	mode    string
	port    string
	token   string // effective token: config/env, else DefaultPublicToken

	mu      sync.Mutex
	conn    net.Conn
	wmu     sync.Mutex
	streams map[uint32]net.Conn
	insp    map[uint32]*httpInspector

	tlog    *tunnelLogger
	attempt int
}

// Run connects, prints the URL, reconnects forever until ctx is done.
func (t *TunnelClient) Run(ctx context.Context) error {
	first := true
	for {
		if ctx.Err() != nil {
			return nil
		}
		err := t.connectOnce(first)
		if ctx.Err() != nil {
			return nil
		}
		if first {
			first = false
		}

		wait := t.backoff()
		kind := "disconnected"
		if strings.Contains(err.Error(), "denied") {
			kind = "error"
		}
		if jsonOut {
			emitEvent(kind, map[string]interface{}{
				"error":    err.Error(),
				"retry_in": wait.Round(time.Millisecond).String(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "[dt] connection lost (%v), retrying in %s\n", err, wait)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

func (t *TunnelClient) backoff() time.Duration {
	t.attempt++
	shift := t.attempt
	if shift > 6 {
		shift = 6
	}
	d := time.Second << uint(shift)
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	jitter := time.Duration(randInt63n(int64(d) / 4))
	return d + jitter
}

// connectOnce performs one full control session.
func (t *TunnelClient) connectOnce(first bool) error {
	host, _, err := net.SplitHostPort(t.cfg.Server)
	if err != nil {
		return err
	}
	conn, err := dialControlTLS(t.cfg.Server, host)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	hello := HelloMsg{
		Version:   1,
		Token:     t.token,
		Subdomain: t.cfg.Sub,
		Mode:      t.mode,
		LocalPort: port2int(t.port),
		Sess:      t.cfg.Sess,
	}
	hb, _ := json.Marshal(hello)
	if err := WriteFrame(conn, PktHello, hb); err != nil {
		return err
	}

	conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	typ, payload, err := ReadFrame(conn)
	if err != nil {
		return err
	}
	if typ == PktDenied {
		return fmt.Errorf("server denied: %s", string(payload))
	}
	if typ != PktWelcome {
		return fmt.Errorf("unexpected frame %d", typ)
	}
	var wel WelcomeMsg
	if err := json.Unmarshal(payload, &wel); err != nil {
		return err
	}
	conn.SetReadDeadline(time.Time{})

	t.mu.Lock()
	t.conn = conn
	t.attempt = 0
	t.mu.Unlock()

	scheme := "https"
	local := "http://127.0.0.1:" + t.port
	if t.mode == "tcp" {
		scheme = "tcp"
		local = "tcp://127.0.0.1:" + t.port
	}
	url := fmt.Sprintf("%s://%s", scheme, wel.Host)
	if jsonOut {
		emitEvent("ready", map[string]interface{}{
			"url":       url,
			"host":      wel.Host,
			"local":     local,
			"mode":      t.mode,
			"localPort": port2int(t.port),
			"reconnect": wel.Reconnect,
			"server":    t.cfg.Server,
		})
	} else if first || wel.Reconnect {
		status := "tunnel online"
		if wel.Reconnect {
			status = "reconnected — session restored"
		}
		fmt.Printf("\n  dreamTunnel — %s\n\n", status)
		fmt.Printf("  Forwarding   %s -> %s\n", url, local)
		fmt.Printf("  Connections  %s\n\n", url)
		fmt.Printf("  Press Ctrl+C to stop\n\n")
	} else {
		fmt.Printf("[dt] %s -> %s\n", local, url)
	}

	return t.readLoop(ctx0(), conn)
}

// ctx0 is set by Run (shared context). Kept simple: package-level current ctx.
var runCtx context.Context

func ctx0() context.Context { return runCtx }

// readLoop multiplexes frame reads and keepalive pings.
func (t *TunnelClient) readLoop(ctx context.Context, conn net.Conn) error {
	done := make(chan error, 1)
	go func() { done <- t.readFrames(conn) }()

	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-done:
			return err
		case <-tick.C:
			if err := t.writeFrame(PktPing, nil); err != nil {
				return err
			}
		}
	}
}

// readFrames processes incoming frames until the conn breaks.
func (t *TunnelClient) readFrames(conn net.Conn) error {
	for {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		typ, payload, err := ReadFrame(conn)
		if err != nil {
			return err
		}
		switch typ {
		case PktPing:
			if err := t.writeFrame(PktPong, nil); err != nil {
				return err
			}
		case PktOpen:
			if len(payload) < 4 {
				continue
			}
			id := GetU32(payload[:4])
			t.openLocal(id)
		case PktData:
			if len(payload) < 4 {
				continue
			}
			id := GetU32(payload[:4])
			if c := t.getStream(id); c != nil {
				data := payload[4:]
				t.mu.Lock()
				ins := t.insp[id]
				t.mu.Unlock()
				if ins != nil {
					// data from the server is the browser's request
					ins.FeedRequest(data)
				}
				if _, err := c.Write(data); err != nil {
					t.closeLocal(id)
				}
			}
		case PktClose:
			if len(payload) >= 4 {
				t.closeLocal(GetU32(payload[:4]))
			}
		}
	}
}

// openLocal dials the local service and pumps local -> tunnel.
func (t *TunnelClient) openLocal(id uint32) {
	addr := "127.0.0.1:" + t.port
	c, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		t.writeFrame(PktClose, u32b(id))
		return
	}
	t.mu.Lock()
	t.streams[id] = c
	var ins *httpInspector
	if t.mode == "http" {
		ins = newHTTPInspector(id, t)
		t.insp[id] = ins
	}
	t.mu.Unlock()

	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := c.Read(buf)
			if n > 0 {
				if ins != nil {
					// bytes read from the local service are the response
					ins.FeedResponse(buf[:n])
				}
				frame := append(u32b(id), buf[:n]...)
				if werr := t.writeFrame(PktData, frame); werr != nil {
					c.Close()
					return
				}
			}
			if err != nil {
				c.Close()
				t.mu.Lock()
				delete(t.streams, id)
				delete(t.insp, id)
				t.mu.Unlock()
				t.writeFrame(PktClose, u32b(id))
				return
			}
		}
	}()
}

func (t *TunnelClient) getStream(id uint32) net.Conn {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.streams[id]
}

func (t *TunnelClient) closeLocal(id uint32) {
	t.mu.Lock()
	c := t.streams[id]
	delete(t.streams, id)
	delete(t.insp, id)
	t.mu.Unlock()
	if c != nil {
		c.Close()
	}
}

func (t *TunnelClient) writeFrame(typ byte, payload []byte) error {
	t.mu.Lock()
	conn := t.conn
	t.mu.Unlock()
	if conn == nil {
		return errors.New("not connected")
	}
	t.wmu.Lock()
	defer t.wmu.Unlock()
	return WriteFrame(conn, typ, payload)
}

func port2int(p string) int {
	var v int
	fmt.Sscanf(p, "%d", &v)
	return v
}

// SetRunCtx wires the shared context (called from Run).
func (t *TunnelClient) SetRunCtx(ctx context.Context) { runCtx = ctx }
