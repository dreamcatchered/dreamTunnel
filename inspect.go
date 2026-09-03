package main

import (
	"bufio"
	"bytes"
	"net/http"
	"sync"
)

// httpInspector observes a proxied local connection: parses the first
// HTTP request (method, path) and the first response (status), then
// reports them so the CLI can log ngrok-style activity lines.
// Bytes are never modified — the stream passes through untouched.
type httpInspector struct {
	id       uint32
	t        *TunnelClient
	mu       sync.Mutex
	reqDone  bool
	respDone bool
}

func newHTTPInspector(id uint32, t *TunnelClient) *httpInspector {
	return &httpInspector{id: id, t: t}
}

// FeedRequest inspects bytes flowing local -> public (the request).
func (h *httpInspector) FeedRequest(chunk []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.reqDone {
		return
	}
	if !bytes.Contains(chunk, []byte("\r\n\r\n")) {
		return
	}
	h.reqDone = true
	rd := bufio.NewReader(bytes.NewReader(chunk))
	req, err := http.ReadRequest(rd)
	if err != nil {
		return
	}
	path := req.URL.Path
	if path == "" {
		path = "/"
	}
	if req.URL.RawQuery != "" {
		path += "?" + req.URL.RawQuery
	}
	h.t.logLine(inspectReq{
		ID:     h.id,
		Method: req.Method,
		Path:   path,
	})
}

// FeedResponse inspects bytes flowing public -> local (the response).
func (h *httpInspector) FeedResponse(chunk []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.respDone {
		return
	}
	if !bytes.Contains(chunk, []byte("\r\n\r\n")) {
		return
	}
	h.respDone = true
	rd := bufio.NewReader(bytes.NewReader(chunk))
	resp, err := http.ReadResponse(rd, nil)
	if err != nil {
		return
	}
	h.t.logLine(inspectResp{
		ID:     h.id,
		Status: resp.StatusCode,
	})
}

type inspectReq struct {
	ID     uint32 `json:"id"`
	Method string `json:"m"`
	Path   string `json:"p"`
}

type inspectResp struct {
	ID     uint32 `json:"id"`
	Status int    `json:"s"`
}
