// Package hosttest is a fake CPA host for tests of the follow transport. It
// answers the host.http.* callbacks the way CPA v8.0.78 does (operations,
// streams of at most 32 KiB per read, cancellation, and its redirect policy:
// follow every redirect, dropping credential-like headers once a hop leaves
// the first request's origin), sends the requests itself with a client that
// uses no proxy, and records every callback.
package hosttest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// chunkSize matches the host's stream read size.
const chunkSize = 32 << 10

// Request is a decoded callback request: the union of the fields the
// follow transport sends.
type Request struct {
	HostCallbackID string              `json:"host_callback_id"`
	OperationID    string              `json:"operation_id"`
	StreamID       string              `json:"stream_id"`
	Method         string              `json:"method"`
	URL            string              `json:"url"`
	Headers        map[string][]string `json:"headers"`
	Body           []byte              `json:"body"`
}

// Call is one recorded callback.
type Call struct {
	Method  string
	Request Request
}

type operation struct {
	callbackID string
	ctx        context.Context
	cancel     context.CancelFunc
	claimed    bool
	stream     string
}

type stream struct {
	op   string
	body io.ReadCloser
}

// Host implements outbound.Host.
type Host struct {
	client *http.Client

	mu      sync.Mutex
	next    int
	ops     map[string]*operation
	streams map[string]*stream
	calls   []Call
}

// New returns an empty fake host.
func New() *Host {
	return &Host{
		client:  &http.Client{Transport: &http.Transport{}, CheckRedirect: checkPluginRedirect},
		ops:     map[string]*operation{},
		streams: map[string]*stream{},
	}
}

// checkPluginRedirect mirrors CPA v8.0.78 internal/pluginhost/http_bridge.go
// checkPluginRedirect / crossOriginRedirect / isPluginCredentialHeader.
func checkPluginRedirect(req *http.Request, via []*http.Request) error {
	cross := !sameOrigin(via[0].URL, req.URL)
	for _, hop := range via[1:] {
		cross = cross || !sameOrigin(via[0].URL, hop.URL)
	}
	if cross {
		for name := range req.Header {
			if credentialHeader(name) {
				delete(req.Header, name)
			}
		}
	}
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	return nil
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && originPort(a) == originPort(b)
}

func originPort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}

func credentialHeader(name string) bool {
	l := strings.ToLower(strings.TrimSpace(name))
	switch l {
	case "authorization", "proxy-authorization", "cookie", "cookie2", "www-authenticate":
		return true
	}
	for _, s := range []string{"api-key", "apikey", "api_key", "token", "secret", "credential"} {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

// Calls returns the callbacks received so far.
func (h *Host) Calls() []Call {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Call(nil), h.calls...)
}

// Requests returns the HTTP requests received through host.http.do_stream.
func (h *Host) Requests() []Request {
	var out []Request
	for _, c := range h.Calls() {
		if c.Method == "host.http.do_stream" {
			out = append(out, c.Request)
		}
	}
	return out
}

// Open is the number of operations the host still holds (not finished,
// canceled or closed).
func (h *Host) Open() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.ops)
}

// Call answers one callback with a JSON envelope.
func (h *Host) Call(method string, raw []byte) ([]byte, error) {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return failure("decode host request: " + err.Error())
	}
	h.mu.Lock()
	h.calls = append(h.calls, Call{method, req})
	h.mu.Unlock()
	switch method {
	case "host.http.operation_open":
		return success(map[string]string{"operation_id": h.open(req.HostCallbackID)})
	case "host.http.do_stream":
		return h.doStream(req)
	case "host.http.stream_read":
		return h.read(req.StreamID)
	case "host.http.stream_close":
		h.closeStream(req.StreamID)
		return success(struct{}{})
	case "host.http.cancel":
		h.finish(req.OperationID)
		return success(struct{}{})
	}
	return failure("unsupported host callback " + method)
}

func (h *Host) open(callbackID string) string {
	ctx, cancel := context.WithCancel(context.Background())
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	id := strconv.Itoa(h.next)
	h.ops[id] = &operation{callbackID: callbackID, ctx: ctx, cancel: cancel}
	return id
}

func (h *Host) doStream(req Request) ([]byte, error) {
	h.mu.Lock()
	op := h.ops[req.OperationID]
	if op == nil || op.claimed || op.callbackID != req.HostCallbackID {
		h.mu.Unlock()
		return failure("host http operation " + strconv.Quote(req.OperationID) + " is not open")
	}
	op.claimed = true
	h.mu.Unlock()

	hr, err := http.NewRequestWithContext(op.ctx, req.Method, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		h.finish(req.OperationID)
		return failure("create host http request: " + err.Error())
	}
	hr.Header = req.Headers
	resp, err := h.client.Do(hr)
	if err != nil {
		h.finish(req.OperationID)
		return failure("execute host http request: " + err.Error())
	}
	h.mu.Lock()
	if h.ops[req.OperationID] != op {
		h.mu.Unlock()
		resp.Body.Close()
		return failure("context canceled")
	}
	h.next++
	id := strconv.Itoa(h.next)
	h.streams[id] = &stream{op: req.OperationID, body: resp.Body}
	op.stream = id
	h.mu.Unlock()
	return success(map[string]any{"status_code": resp.StatusCode, "headers": resp.Header, "stream_id": id})
}

// read returns the next chunk. Like CPA, a stream whose operation was
// canceled just ends: done without an error.
func (h *Host) read(id string) ([]byte, error) {
	h.mu.Lock()
	s := h.streams[id]
	var ctx context.Context
	if s != nil {
		ctx = h.ops[s.op].ctx
	}
	h.mu.Unlock()
	if s == nil {
		return failure("http stream " + id + " is not open")
	}
	buf := make([]byte, chunkSize)
	n, err := s.body.Read(buf)
	if n > 0 {
		return success(map[string]any{"payload": buf[:n]})
	}
	h.closeStream(id)
	if err == nil || errors.Is(err, io.EOF) || ctx.Err() != nil {
		return success(map[string]any{"done": true})
	}
	return success(map[string]any{"error": err.Error(), "done": true})
}

func (h *Host) closeStream(id string) {
	h.mu.Lock()
	s := h.streams[id]
	h.mu.Unlock()
	if s != nil {
		h.finish(s.op)
	}
}

// finish ends an operation: cancels its request and drops its stream.
func (h *Host) finish(opID string) {
	h.mu.Lock()
	op := h.ops[opID]
	delete(h.ops, opID)
	var body io.ReadCloser
	if op != nil {
		if s := h.streams[op.stream]; s != nil {
			body = s.body
			delete(h.streams, op.stream)
		}
	}
	h.mu.Unlock()
	if op != nil {
		op.cancel()
	}
	if body != nil {
		body.Close()
	}
}

func success(v any) ([]byte, error) {
	res, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"ok": true, "result": json.RawMessage(res)})
}

func failure(msg string) ([]byte, error) {
	return json.Marshal(map[string]any{"ok": false, "error": map[string]string{"code": "host_call_failed", "message": msg}})
}
