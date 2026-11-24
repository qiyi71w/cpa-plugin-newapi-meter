package outbound

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// Host callbacks of CPA plugin ABI 1 used by the follow transport. CPA sends
// these requests with its own HTTP client, so they use its proxy-url (or,
// without one, its default transport and the CPA process environment).
const (
	methodOperationOpen = "host.http.operation_open"
	methodDoStream      = "host.http.do_stream"
	methodStreamRead    = "host.http.stream_read"
	methodStreamClose   = "host.http.stream_close"
	methodCancel        = "host.http.cancel"
)

// The host applies no timeout and host.http.do buffers whole bodies, so the
// follow transport keeps both limits itself: every request runs as a
// cancelable host operation that is canceled when ctx ends, and the body is
// pulled chunk by chunk through a host stream that is closed as soon as it
// passes the response cap.

type hostOperation struct {
	HostCallbackID string `json:"host_callback_id,omitempty"`
	OperationID    string `json:"operation_id,omitempty"`
}

type hostHTTPRequest struct {
	hostOperation
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    []byte              `json:"body,omitempty"`
}

type hostStreamHead struct {
	StatusCode int         `json:"status_code"`
	Headers    http.Header `json:"headers"`
	StreamID   string      `json:"stream_id"`
}

type hostStreamRef struct {
	StreamID string `json:"stream_id"`
}

type hostStreamChunk struct {
	Payload []byte `json:"payload"`
	Error   string `json:"error"`
	Done    bool   `json:"done"`
}

type hostEnvelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// hostError is a failure reported by the host. Its text may quote the URL,
// so it only ever feeds classifyHost and never reaches an Error.
type hostError string

func (e hostError) Error() string { return string(e) }

type result struct {
	resp *Response
	err  error
}

func (c *Client) doFollow(ctx context.Context, req Request) (*Response, error) {
	method := req.method()
	if _, err := http.NewRequestWithContext(ctx, method, req.URL, nil); err != nil {
		return nil, &Error{ErrRequest, errors.New("invalid request")}
	}
	if c.host == nil {
		return nil, &Error{ErrNetwork, errors.New("host transport unavailable")}
	}
	if err := ctx.Err(); err != nil {
		return nil, classifyTransport(ctx, err)
	}
	call, _ := CallFrom(ctx)
	op := hostOperation{HostCallbackID: call.HostCallbackID}
	if err := c.hostCall(methodOperationOpen, op, &op); err != nil || op.OperationID == "" {
		return nil, classifyHost(err)
	}

	done := make(chan result, 1)
	go func() {
		resp, err := c.runOperation(hostHTTPRequest{hostOperation: op, Method: method, URL: req.URL, Headers: req.Header, Body: req.Body}, req.limit())
		done <- result{resp, err}
	}()
	select {
	case r := <-done:
		if r.err != nil && ctx.Err() != nil {
			return nil, classifyTransport(ctx, ctx.Err())
		}
		return r.resp, r.err
	case <-ctx.Done():
		// The host ends the in-flight request (and its stream) for this
		// operation; runOperation then returns and its result is dropped.
		c.cancel(op)
		return nil, classifyTransport(ctx, ctx.Err())
	}
}

// runOperation sends the request as operation op and reads at most limit
// bytes of its body.
func (c *Client) runOperation(req hostHTTPRequest, limit int64) (*Response, error) {
	var head hostStreamHead
	if err := c.hostCall(methodDoStream, req, &head); err != nil {
		c.cancel(req.hostOperation) // an operation the host never claimed stays open otherwise
		return nil, classifyHost(err)
	}
	if head.StreamID == "" {
		c.cancel(req.hostOperation)
		return nil, &Error{ErrNetwork, errors.New("host returned no stream")}
	}
	size := int64(-1)
	if v := head.Headers.Get("Content-Length"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			size = n
		}
	}
	if size > limit {
		c.closeStream(head.StreamID)
		return nil, &Error{ErrTooLarge, errors.New("response body too large")}
	}
	var body []byte
	if size > 0 {
		body = make([]byte, 0, size)
	}
	ref := hostStreamRef{head.StreamID}
	for {
		var chunk hostStreamChunk
		if err := c.hostCall(methodStreamRead, ref, &chunk); err != nil {
			c.closeStream(head.StreamID)
			return nil, classifyHost(err)
		}
		if chunk.Error != "" {
			return nil, classifyHost(hostError(chunk.Error))
		}
		body = append(body, chunk.Payload...)
		if int64(len(body)) > limit {
			c.closeStream(head.StreamID)
			return nil, &Error{ErrTooLarge, errors.New("response body too large")}
		}
		if chunk.Done {
			break
		}
	}
	// A host stream also ends without an error when its operation is
	// canceled; a short body is never passed on as a complete one.
	if size >= 0 && int64(len(body)) != size {
		return nil, &Error{ErrNetwork, errors.New("response body truncated")}
	}
	if body == nil {
		body = []byte{}
	}
	return &Response{StatusCode: head.StatusCode, Header: head.Headers, Body: body}, nil
}

func (c *Client) cancel(op hostOperation) { _ = c.hostCall(methodCancel, op, nil) }

func (c *Client) closeStream(id string) { _ = c.hostCall(methodStreamClose, hostStreamRef{id}, nil) }

// hostCall sends one callback and decodes its result into out.
func (c *Client) hostCall(method string, in, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	resp, err := c.host.Call(method, raw)
	if err != nil {
		return err
	}
	var env hostEnvelope
	if err := json.Unmarshal(resp, &env); err != nil {
		return errors.New("invalid host response")
	}
	if !env.OK {
		if env.Error != nil {
			return hostError(env.Error.Message)
		}
		return hostError("")
	}
	if out == nil || len(env.Result) == 0 {
		return nil
	}
	return json.Unmarshal(env.Result, out)
}

// classifyHost maps a host failure onto the same kinds the direct transport
// reports. The host only passes Go error text, so the category is read from
// the standard library's wording; the text itself is dropped.
func classifyHost(err error) *Error {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	has := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(msg, s) {
				return true
			}
		}
		return false
	}
	switch {
	case has("no such host", "lookup "):
		return &Error{ErrDNS, errors.New("dns lookup failed")}
	case has("tls:", "x509:"):
		return &Error{ErrTLS, errors.New("tls handshake failed")}
	case has("timeout", "deadline exceeded"):
		return &Error{ErrTimeout, errors.New("transport timeout")}
	case has("dial ", "proxyconnect", "connection refused", "no route to host", "network is unreachable"):
		return &Error{ErrConnect, errors.New("connection failed")}
	}
	return &Error{ErrNetwork, errors.New("network error")}
}
