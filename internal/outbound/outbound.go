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
	"net/netip"
	"net/url"
	"strings"

	"golang.org/x/net/http/httpguts"
	"golang.org/x/net/idna"
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

// Host sends one CPA host callback (ABI 1: method name plus JSON request) and
// returns the host's JSON envelope. cmd/newapi-meter implements it over the
// host API CPA passes to cliproxy_plugin_init.
type Host interface {
	Call(method string, request []byte) ([]byte, error)
}

// Client is the plugin's outbound entry.
type Client struct {
	direct *http.Client
	host   Host
}

// New returns a Client. Follow requests go through host; a nil host makes
// them fail as network errors rather than silently going direct. The direct
// transport ignores every proxy setting and only follows redirects that stay
// on the request's site (see checkRedirect).
func New(host Host) *Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	return &Client{direct: &http.Client{Transport: t, CheckRedirect: checkRedirect}, host: host}
}

// maxRedirects matches net/http's default limit.
const maxRedirects = 10

// checkRedirect follows a redirect only while every hop stays on the site of
// the original request and never goes from https back to http. net/http
// rebuilds each hop from the original request, so following a redirect
// elsewhere would hand that site the headers (all but a few), the body on
// 307/308 and, through Referer, the original URL with its query. A redirect
// that is not followed returns the 3xx response itself, which is classified
// like any other non-2xx answer.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errors.New("stopped after 10 redirects")
	}
	last := via[len(via)-1].URL
	if !sameSite(via[0].URL, req.URL) || (strings.EqualFold(last.Scheme, "https") && strings.EqualFold(req.URL.Scheme, "http")) {
		return http.ErrUseLastResponse
	}
	return nil
}

// sameSite reports whether to is on the site of from: same scheme, host and
// port, or the https upgrade of an http URL on its default port.
func sameSite(from, to *url.URL) bool {
	if !sameHost(from.Hostname(), to.Hostname()) {
		return false
	}
	fs, ts := strings.ToLower(from.Scheme), strings.ToLower(to.Scheme)
	fp, tp := port(from), port(to)
	if fs == ts {
		return fp == tp
	}
	return fs == "http" && ts == "https" && fp == "80" && tp == "443"
}

// sameHost reports whether a and b, URL host names without brackets or port,
// are the same host. IP literals are compared first, as parsed addresses with
// the zone exactly; an address never matches a name. Names must agree on
// both the form the transport dials and the form net/http writes into Host,
// so a redirect never reaches another virtual host on the same address.
func sameHost(a, b string) bool {
	ipa, errA := netip.ParseAddr(a)
	ipb, errB := netip.ParseAddr(b)
	if errA == nil || errB == nil {
		return errA == nil && errB == nil && ipa == ipb
	}
	return a != "" && b != "" && sameForm(a, b, dialName) && sameForm(a, b, httpguts.PunycodeHostPort)
}

// dialName is the name net/http dials: a non-ASCII name after IDNA Lookup
// conversion, which maps characters (é and É, U+212A and k) to one form.
func dialName(h string) (string, error) {
	if isASCII(h) {
		return h, nil
	}
	return idna.Lookup.ToASCII(h)
}

// sameForm reports whether a and b convert to the same ASCII form, ignoring
// ASCII case only (Unicode folding would equate σ/ς). A name that fails to
// convert matches nothing.
func sameForm(a, b string, form func(string) (string, error)) bool {
	fa, err := form(a)
	if err != nil {
		return false
	}
	fb, err := form(b)
	if err != nil || len(fa) != len(fb) || !isASCII(fa) || !isASCII(fb) {
		return false
	}
	for i := range len(fa) {
		if lowerASCII(fa[i]) != lowerASCII(fb[i]) {
			return false
		}
	}
	return true
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func lowerASCII(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

func port(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	}
	return ""
}

// Do sends req with the transport its Proxy option selects: Direct uses the
// plugin's own client, anything else (Follow, empty) relays through the host.
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	if req.Proxy == Direct {
		return c.doDirect(ctx, req)
	}
	return c.doFollow(ctx, req)
}

// CloseIdle releases pooled connections of the direct transport.
func (c *Client) CloseIdle() { c.direct.CloseIdleConnections() }

func (r Request) method() string {
	if r.Method == "" {
		return http.MethodGet
	}
	return r.Method
}

func (r Request) limit() int64 {
	if r.MaxResponseBytes <= 0 {
		return DefaultMaxResponseBytes
	}
	return r.MaxResponseBytes
}

func (c *Client) doDirect(ctx context.Context, req Request) (*Response, error) {
	var body io.Reader
	if req.Body != nil {
		body = bytes.NewReader(req.Body)
	}
	hr, err := http.NewRequestWithContext(ctx, req.method(), req.URL, body)
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
	limit := req.limit()
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
