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
		fmt.Fprintf(os.Stdout, "%-7s %-23s %d %s\n", " ", statusText(m.Status), m.Status, d)
	}
}

func statusText(code int) string {
	switch code {
	case 200:
		return "OK"
	case 201:
		return "Created"
	case 204:
		return "No Content"
	case 301:
		return "Moved Permanently"
	case 302:
		return "Found"
	case 304:
		return "Not Modified"
	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 405:
		return "Method Not Allowed"
	case 408:
		return "Request Timeout"
	case 429:
		return "Too Many Requests"
	case 500:
		return "Internal Server Error"
	case 502:
		return "Bad Gateway"
	case 503:
		return "Service Unavailable"
	case 504:
		return "Gateway Timeout"
	}
	return fmt.Sprintf("Status %d", code)
}

