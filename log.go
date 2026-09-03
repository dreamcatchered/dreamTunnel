package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// tunnelLogger renders ngrok-style activity lines:
//
//	Forwarding   https://myapp.maiuni.online -> http://127.0.0.1:8080
//	GET /            200 OK           12.4ms
//	POST /api/login  401 Unauthorized  3.1ms
type tunnelLogger struct {
	mu      sync.Mutex
	started map[uint32]time.Time
}

func newTunnelLogger() *tunnelLogger {
	return &tunnelLogger{started: map[uint32]time.Time{}}
}

// logLine prints one activity line. Called from the inspector path.
func (t *TunnelClient) logLine(v interface{}) {
	switch m := v.(type) {
	case inspectReq:
		t.tlog.mu.Lock()
		t.tlog.started[m.ID] = time.Now()
		t.tlog.mu.Unlock()
		fmt.Fprintf(os.Stdout, "%-7s %s\n", m.Method, m.Path)
	case inspectResp:
		t.tlog.mu.Lock()
		start := t.tlog.started[m.ID]
		delete(t.tlog.started, m.ID)
		t.tlog.mu.Unlock()
		d := time.Since(start).Round(time.Millisecond)
		fmt.Fprintf(os.Stdout, "        %-4s %d %s\n", statusWord(m.Status), m.Status, d)
	}
}

// statusWord is the short reason phrase (OK, Not Found...).
func statusWord(code int) string {
	switch code {
	case 200:
		return "OK"
	case 201:
		return "Created"
	case 204:
		return "NoContent"
	case 301:
		return "Moved"
	case 302:
		return "Found"
	case 304:
		return "NotMod"
	case 400:
		return "BadReq"
	case 401:
		return "Unauth"
	case 403:
		return "Forbidden"
	case 404:
		return "NotFound"
	case 405:
		return "NoMethod"
	case 429:
		return "RateLim"
	case 500:
		return "SrvErr"
	case 502:
		return "BadGw"
	case 503:
		return "Unavail"
	case 504:
		return "GwTimeout"
	}
	return "Status"
}


