// Package outbound is the single exit for every request the plugin sends to a
// site. Callers describe the request, carry the management-call context and
// their own deadline in ctx, and get back either the upstream response or a
// classified transport error; status.Classify turns both into failure codes.
package outbound

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
)

// Proxy is an account's proxy option.
type Proxy string

const (
	// Follow routes through CPA's proxy-url via the host.http.* callbacks.
	Follow Proxy = "follow"
	// Direct uses the plugin's own client and never any proxy, including
	// HTTP(S)_PROXY / NO_PROXY from the environment.
	Direct Proxy = "direct"
)

// DefaultMaxResponseBytes bounds a response body when Request.MaxResponseBytes is 0.
const DefaultMaxResponseBytes int64 = 8 << 20

// Request is one outbound HTTP request.
type Request struct {
	Method string
	URL    string
	Header http.Header
	Body   []byte
	// Proxy selects the transport. Empty means Follow.
	Proxy Proxy
	// MaxResponseBytes caps the response body; larger bodies fail with ErrTooLarge.
	MaxResponseBytes int64
}

// Response is a fully read upstream response.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// ErrorKind classifies a request that produced no usable response.
type ErrorKind string

const (
	ErrTimeout  ErrorKind = "timeout"   // caller deadline or transport timeout
	ErrCanceled ErrorKind = "canceled"  // caller canceled ctx
	ErrDNS      ErrorKind = "dns"       // host name did not resolve
	ErrTLS      ErrorKind = "tls"       // TLS handshake or certificate failure
	ErrConnect  ErrorKind = "connect"   // connection refused, reset or unreachable
	ErrTooLarge ErrorKind = "too_large" // body exceeded MaxResponseBytes
	ErrRequest  ErrorKind = "request"   // the request itself was invalid (bad URL)
	ErrNetwork  ErrorKind = "network"   // any other transport failure
)

// Error is returned instead of a Response when the transport failed.
// It never carries request headers, bodies or response bodies.
type Error struct {
	Kind ErrorKind
	Err  error
}

func (e *Error) Error() string { return "outbound " + string(e.Kind) + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// Doer is the outbound entry contract. Implementations must honor ctx
// cancellation and deadline and must not log request or response content.
type Doer interface {
	Do(ctx context.Context, req Request) (*Response, error)
}

// Call carries the management call that triggered outbound work, so a
// transport that relays through the host can address the right host context.
type Call struct {
	// HostCallbackID is the host_callback_id of the management.handle call.
	HostCallbackID string
}

type callKey struct{}

// WithCall attaches the management call to ctx.
func WithCall(ctx context.Context, c Call) context.Context {
	return context.WithValue(ctx, callKey{}, c)
}

// CallFrom returns the management call attached to ctx.
func CallFrom(ctx context.Context) (Call, bool) {
	c, ok := ctx.Value(callKey{}).(Call)
	return c, ok
}

// Client is the plugin's outbound entry.
type Client struct {
	direct *http.Client
}

// New returns a Client whose direct transport ignores every proxy setting.
func New() *Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	return &Client{direct: &http.Client{Transport: t}}
}

// Do sends req with the transport its Proxy option selects. This build only
// has the direct transport, so Follow is served by it as well.
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	return c.doDirect(ctx, req)
}

// CloseIdle releases pooled connections.
func (c *Client) CloseIdle() { c.direct.CloseIdleConnections() }

func (c *Client) doDirect(ctx context.Context, req Request) (*Response, error) {
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if req.Body != nil {
		body = bytes.NewReader(req.Body)
	}
	hr, err := http.NewRequestWithContext(ctx, method, req.URL, body)
	if err != nil {
		return nil, &Error{ErrRequest, errors.New("invalid request")}
	}
	for k, vs := range req.Header {
		hr.Header[k] = append([]string(nil), vs...)
	}
	resp, err := c.direct.Do(hr)
	if err != nil {
		return nil, classifyTransport(ctx, err)
	}
	defer resp.Body.Close()
	limit := req.MaxResponseBytes
	if limit <= 0 {
		limit = DefaultMaxResponseBytes
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, classifyTransport(ctx, err)
	}
	if int64(len(data)) > limit {
		return nil, &Error{ErrTooLarge, errors.New("response body too large")}
	}
	return &Response{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: data}, nil
}

func classifyTransport(ctx context.Context, err error) *Error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return &Error{ErrTimeout, ctxErr}
		}
		return &Error{ErrCanceled, ctxErr}
	}
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var recordErr tls.RecordHeaderError
	var alertErr tls.AlertError
	var netErr net.Error
	// Only the error category survives; *url.Error text would repeat the URL.
	switch {
	case errors.As(err, &dnsErr):
		return &Error{ErrDNS, errors.New("dns lookup failed")}
	case errors.As(err, &certErr), errors.As(err, &recordErr), errors.As(err, &alertErr):
		return &Error{ErrTLS, errors.New("tls handshake failed")}
	case errors.As(err, &netErr) && netErr.Timeout():
		return &Error{ErrTimeout, errors.New("transport timeout")}
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Op == "dial" {
			return &Error{ErrConnect, errors.New("connection failed")}
		}
		if opErr.Op == "remote error" {
			return &Error{ErrTLS, errors.New("tls handshake failed")}
		}
	}
	return &Error{ErrNetwork, errors.New("network error")}
}
