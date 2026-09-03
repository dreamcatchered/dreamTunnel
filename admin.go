package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ---- admin state helpers on TunnelServer ----

// hostOnly strips the port from an addr string.
func hostOnly(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

// IsIPBanned reports whether ip is currently banned.
func (s *TunnelServer) IsIPBanned(ip string) bool {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	until, ok := s.bannedIP[ip]
	return ok && time.Now().Before(until)
}

// BanIP adds ip to the ban list.
func (s *TunnelServer) BanIP(ip string) {
	s.adminMu.Lock()
	s.bannedIP[ip] = time.Now().Add(365 * 24 * time.Hour)
	s.adminMu.Unlock()
	log.Printf("[dt] admin banned ip %s", ip)
	// kill active connections from this IP
	for _, cc := range s.listClients() {
		if cc.ip == ip {
			s.KillSession(cc)
		}
	}
}

// UnbanIP removes ip from the ban list.
func (s *TunnelServer) UnbanIP(ip string) {
	s.adminMu.Lock()
	delete(s.bannedIP, ip)
	s.adminMu.Unlock()
}

// BanList returns currently banned IPs with expiry.
func (s *TunnelServer) BanList() map[string]time.Time {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	out := map[string]time.Time{}
	for k, v := range s.bannedIP {
		out[k] = v
	}
	return out
}

// authBlocked reports whether the IP is temporarily locked out.
func (s *TunnelServer) authBlocked(ip string) bool {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	b, ok := s.rateAuth[ip]
	return ok && b.hits >= maxAuthFails && time.Now().Before(b.blocked)
}

// authFail records a failed auth attempt; locks the IP out after maxAuthFails.
func (s *TunnelServer) authFail(ip string) {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	b := s.rateAuth[ip]
	if b == nil {
		b = &rateBucket{}
		s.rateAuth[ip] = b
	}
	if !b.blocked.IsZero() && time.Now().After(b.blocked) {
		b = &rateBucket{}
		s.rateAuth[ip] = b
	}
	b.hits++
	if b.hits >= maxAuthFails {
		b.blocked = time.Now().Add(authBanTime)
	}
	s.stats.AuthFails++
}

// authOK clears the failure counter.
func (s *TunnelServer) authOK(ip string) {
	s.adminMu.Lock()
	delete(s.rateAuth, ip)
	s.adminMu.Unlock()
}

// recordSession upserts a history entry for an active session.
func (s *TunnelServer) recordSession(cc *ClientConn, resumed bool) {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	s.stats.TotalSessions++
	if !resumed {
		s.history = append(s.history, HistoryEntry{
			Sess: cc.sess, Host: cc.host, Mode: cc.mode, Port: cc.port,
			IP: cc.ip, Start: time.Now(), Active: true,
		})
		if len(s.history) > historyLimit {
			s.history = s.history[len(s.history)-historyLimit:]
		}
	}
}

// finishSession marks the history entry closed (by sess+host).
func (s *TunnelServer) finishSession(cc *ClientConn, killed bool) {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	cc.statsMu.Lock()
	st := HistoryEntry{
		Streams: cc.streamsN, BytesIn: cc.bytesIn, BytesOut: cc.bytesOut,
	}
	cc.statsMu.Unlock()
	s.stats.TotalStreams += st.Streams
	s.stats.TotalBytesIn += st.BytesIn
	s.stats.TotalBytesOut += st.BytesOut
	for i := range s.history {
		h := &s.history[i]
		if h.Sess == cc.sess && h.Host == cc.host && h.Active {
			h.Active = false
			h.End = time.Now()
			h.Streams = st.Streams
			h.BytesIn = st.BytesIn
			h.BytesOut = st.BytesOut
			h.Killed = killed
			return
		}
	}
}

// listClients returns a snapshot of active client conns.
func (s *TunnelServer) listClients() []*ClientConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*ClientConn, 0, len(s.byHost))
	for _, cc := range s.byHost {
		out = append(out, cc)
	}
	return out
}

// KillSession force-closes a client's control connection.
func (s *TunnelServer) KillSession(cc *ClientConn) {
	s.finishSession(cc, true)
	cc.closed = true
	cc.closeStreams()
	cc.conn.Close()
	s.Unregister(cc)
	log.Printf("[dt] admin killed session %s host=%s ip=%s", cc.sess, cc.host, cc.ip)
}

// ---- admin HTTP panel ----

type adminServer struct {
	srv    *TunnelServer
	user   string
	passHash string // sha256 hex of "user:pass"
	salt   string

	mu       sync.Mutex
	sessions map[string]time.Time // token -> expiry
	loginFail map[string]*rateBucket
}

func newAdminServer(srv *TunnelServer, user, pass string) *adminServer {
	saltBytes := make([]byte, 16)
	rand.Read(saltBytes)
	a := &adminServer{
		srv:      srv,
		user:     user,
		passHash: adminHash(user, pass, hex.EncodeToString(saltBytes)),
		salt:     hex.EncodeToString(saltBytes),
		sessions: map[string]time.Time{},
		loginFail: map[string]*rateBucket{},
	}
	return a
}

func adminHash(user, pass, salt string) string {
	return sha256hex(user + ":" + pass + ":" + salt)
}

func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// checkLogin compares credentials in constant time.
func (a *adminServer) checkLogin(user, pass string) bool {
	h := adminHash(user, pass, a.salt)
	return subtle.ConstantTimeCompare([]byte(h), []byte(a.passHash)) == 1 &&
		subtle.ConstantTimeCompare([]byte(user), []byte(a.user)) == 1
}

// issueSession creates an admin session token valid 12h.
func (a *adminServer) issueSession() string {
	b := make([]byte, 24)
	rand.Read(b)
	tok := base64.RawURLEncoding.EncodeToString(b)
	a.mu.Lock()
	a.sessions[tok] = time.Now().Add(12 * time.Hour)
	a.mu.Unlock()
	return tok
}

// validSession checks the session token.
func (a *adminServer) validSession(tok string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	exp, ok := a.sessions[tok]
	if !ok || time.Now().After(exp) {
		return false
	}
	return true
}

// revokeSession removes a session token.
func (a *adminServer) revokeSession(tok string) {
	a.mu.Lock()
	delete(a.sessions, tok)
	a.mu.Unlock()
}

// loginLimited applies brute-force protection to the admin login.
func (a *adminServer) loginLimited(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.loginFail[ip]
	return b != nil && b.hits >= maxAuthFails && time.Now().Before(b.blocked)
}

func (a *adminServer) loginFailed(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.loginFail[ip]
	if b == nil {
		b = &rateBucket{}
		a.loginFail[ip] = b
	}
	// reset the window only when an active block has expired
	if !b.blocked.IsZero() && time.Now().After(b.blocked) {
		b = &rateBucket{}
		a.loginFail[ip] = b
	}
	b.hits++
	if b.hits >= maxAuthFails {
		b.blocked = time.Now().Add(authBanTime)
	}
}

func (a *adminServer) loginOK(ip string) {
	a.mu.Lock()
	delete(a.loginFail, ip)
	a.mu.Unlock()
}

// Handler returns the admin HTTP mux (mount behind 127.0.0.1 only).
func (a *adminServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.page)
	mux.HandleFunc("/api/login", a.apiLogin)
	mux.HandleFunc("/api/logout", a.apiLogout)
	mux.HandleFunc("/api/state", a.apiState)
	mux.HandleFunc("/api/kill", a.apiKill)
	mux.HandleFunc("/api/ban", a.apiBan)
	mux.HandleFunc("/api/unban", a.apiUnban)
	return mux
}

func (a *adminServer) page(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(adminHTML))
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func (a *adminServer) apiLogin(w http.ResponseWriter, r *http.Request) {
	ip := hostOnly(r.RemoteAddr)
	if a.loginLimited(ip) {
		writeJSON(w, map[string]string{"error": "Слишком много попыток. Подождите 15 минут."})
		return
	}
	var creds struct{ User, Pass string }
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		writeJSON(w, map[string]string{"error": "Неверный запрос"})
		return
	}
	if !a.checkLogin(creds.User, creds.Pass) {
		a.loginFailed(ip)
		time.Sleep(300 * time.Millisecond)
		writeJSON(w, map[string]string{"error": "Неверный логин или пароль"})
		return
	}
	a.loginOK(ip)
	tok := a.issueSession()
	http.SetCookie(w, &http.Cookie{Name: "dtadmin", Value: tok, HttpOnly: true, SameSite: http.SameSiteStrictMode, Path: "/", MaxAge: 43200})
	writeJSON(w, map[string]string{"ok": "1"})
}

func (a *adminServer) apiLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("dtadmin"); err == nil {
		a.revokeSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "dtadmin", Value: "", MaxAge: -1, Path: "/"})
	writeJSON(w, map[string]string{"ok": "1"})
}

func (a *adminServer) authed(r *http.Request) bool {
	c, err := r.Cookie("dtadmin")
	return err == nil && a.validSession(c.Value)
}

func (a *adminServer) apiState(w http.ResponseWriter, r *http.Request) {
	if !a.authed(r) {
		writeJSON(w, map[string]string{"error": "unauthorized"})
		return
	}
	clients := a.srv.listClients()
	type cl struct {
		Sess, Host, Mode, IP string
		Port                 int
		Started, LastSeen    time.Time
		Streams              int64
		BytesIn, BytesOut    int64
		StreamsNow           int
	}
	cls := make([]cl, 0, len(clients))
	for _, cc := range clients {
		cc.smu.Lock()
		now := len(cc.streams)
		cc.smu.Unlock()
		cc.statsMu.Lock()
		si, so, sn := cc.bytesIn, cc.bytesOut, cc.streamsN
		cc.statsMu.Unlock()
		cls = append(cls, cl{
			Sess: cc.sess, Host: cc.host, Mode: cc.mode, IP: cc.ip, Port: cc.port,
			Started: cc.started, LastSeen: cc.lastSeen, Streams: sn,
			BytesIn: si, BytesOut: so, StreamsNow: now,
		})
	}
	a.srv.adminMu.Lock()
	hist := make([]HistoryEntry, len(a.srv.history))
	copy(hist, a.srv.history)
	stats := a.srv.stats
	a.srv.adminMu.Unlock()
	// newest first
	for i, j := 0, len(hist)-1; i < j; i, j = i+1, j-1 {
		hist[i], hist[j] = hist[j], hist[i]
	}
	bans := map[string]string{}
	for ip, until := range a.srv.BanList() {
		bans[ip] = until.Format("2006-01-02 15:04")
	}
	writeJSON(w, map[string]interface{}{
		"clients": cls, "history": hist, "stats": stats, "bans": bans,
		"filter": a.srv.Filter.Enabled, "domain": TunnelDomain,
	})
}

func (a *adminServer) apiKill(w http.ResponseWriter, r *http.Request) {
	if !a.authed(r) {
		writeJSON(w, map[string]string{"error": "unauthorized"})
		return
	}
	var req struct{ Sess, Host string }
	json.NewDecoder(r.Body).Decode(&req)
	for _, cc := range a.srv.listClients() {
		if (req.Sess == "" || cc.sess == req.Sess) && (req.Host == "" || cc.host == req.Host) {
			a.srv.KillSession(cc)
			writeJSON(w, map[string]string{"ok": "killed"})
			return
		}
	}
	writeJSON(w, map[string]string{"error": "not found"})
}

func (a *adminServer) apiBan(w http.ResponseWriter, r *http.Request) {
	if !a.authed(r) {
		writeJSON(w, map[string]string{"error": "unauthorized"})
		return
	}
	var req struct{ IP string }
	json.NewDecoder(r.Body).Decode(&req)
	ip := strings.TrimSpace(req.IP)
	if ip == "" {
		writeJSON(w, map[string]string{"error": "ip required"})
		return
	}
	a.srv.BanIP(ip)
	writeJSON(w, map[string]string{"ok": "banned"})
}

func (a *adminServer) apiUnban(w http.ResponseWriter, r *http.Request) {
	if !a.authed(r) {
		writeJSON(w, map[string]string{"error": "unauthorized"})
		return
	}
	var req struct{ IP string }
	json.NewDecoder(r.Body).Decode(&req)
	a.srv.UnbanIP(strings.TrimSpace(req.IP))
	writeJSON(w, map[string]string{"ok": "unbanned"})
}

