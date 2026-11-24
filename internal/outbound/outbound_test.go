package outbound

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/outbound/hosttest"
)

func kindOf(t *testing.T, err error) ErrorKind {
	t.Helper()
	var oe *Error
	if !errors.As(err, &oe) {
		t.Fatalf("err = %v, want *Error", err)
	}
	return oe.Kind
}

func TestDirectIgnoresEnvironmentProxyAndHost(t *testing.T) {
	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxied.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)
	t.Setenv("https_proxy", proxy.URL)
	t.Setenv("NO_PROXY", "")

	host := hosttest.New()
	_, err := New(host).Do(context.Background(), Request{URL: "http://newapi-meter-proxy-check.invalid/api/status", Proxy: Direct})
	if k := kindOf(t, err); k != ErrDNS {
		t.Fatalf("kind = %s, want a direct DNS failure", k)
	}
	if n := proxied.Load(); n != 0 {
		t.Fatalf("environment proxy received %d requests", n)
	}
	if calls := host.Calls(); len(calls) != 0 {
		t.Fatalf("direct request reached the host: %+v", calls)
	}
}

func TestFollowSendsTheRequestThroughTheHost(t *testing.T) {
	type seen struct {
		method, uri, auth, user, custom string
		body                            []byte
	}
	got := make(chan seen, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- seen{r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), r.Header.Get("New-Api-User"), r.Header.Get("X-Custom"), b}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	for _, mode := range []Proxy{Follow, ""} {
		host := hosttest.New()
		ctx := WithCall(context.Background(), Call{HostCallbackID: "cb-7"})
		req := Request{
			Method: http.MethodPost,
			URL:    srv.URL + "/v1/usage?key=q-synthetic",
			Header: http.Header{"Authorization": {"Bearer synthetic"}, "New-Api-User": {"42"}, "X-Custom": {"a", "b"}},
			Body:   []byte(`{"token":"b-synthetic"}`),
			Proxy:  mode,
		}
		resp, err := New(host).Do(ctx, req)
		if err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
		if resp.StatusCode != http.StatusAccepted || string(resp.Body) != `{"success":true}` || resp.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("mode %q: response %d %v %q", mode, resp.StatusCode, resp.Header, resp.Body)
		}
		s := <-got
		if s.method != http.MethodPost || s.uri != "/v1/usage?key=q-synthetic" || s.auth != "Bearer synthetic" || s.user != "42" || s.custom != "a" || string(s.body) != `{"token":"b-synthetic"}` {
			t.Fatalf("mode %q: upstream saw %+v", mode, s)
		}

		calls := host.Calls()
		if len(calls) < 3 || calls[0].Method != "host.http.operation_open" || calls[1].Method != "host.http.do_stream" {
			t.Fatalf("mode %q: calls %+v", mode, calls)
		}
		if calls[0].Request.HostCallbackID != "cb-7" {
			t.Fatalf("operation opened for callback %q", calls[0].Request.HostCallbackID)
		}
		hr := calls[1].Request
		if hr.HostCallbackID != "cb-7" || hr.OperationID == "" || hr.Method != http.MethodPost || hr.URL != req.URL ||
			strings.Join(hr.Headers["X-Custom"], ",") != "a,b" || !bytes.Equal(hr.Body, req.Body) {
			t.Fatalf("mode %q: do_stream request %+v", mode, hr)
		}
		for _, c := range calls[2:] {
			if c.Method != "host.http.stream_read" {
				t.Fatalf("mode %q: unexpected callback %s", mode, c.Method)
			}
		}
		if n := host.Open(); n != 0 {
			t.Fatalf("mode %q: %d host operations left open", mode, n)
		}
	}
}

func TestFollowDeadlineCancelsTheHostOperation(t *testing.T) {
	cases := map[string]func(w http.ResponseWriter){
		"before the response": func(http.ResponseWriter) {},
		"while reading the body": func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":`))
			w.(http.Flusher).Flush()
		},
	}
	for name, start := range cases {
		t.Run(name, func(t *testing.T) {
			canceled := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				start(w)
				<-r.Context().Done()
				close(canceled)
			}))
			defer srv.Close()
			host := hosttest.New()
			ctx := WithCall(context.Background(), Call{HostCallbackID: "cb-9"})
			ctx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
			defer cancel()

			began := time.Now()
			_, err := New(host).Do(ctx, Request{URL: srv.URL})
			if k := kindOf(t, err); k != ErrTimeout {
				t.Fatalf("kind = %s, want timeout", k)
			}
			if took := time.Since(began); took > time.Second {
				t.Fatalf("returned after %v, deadline was 150ms", took)
			}
			select {
			case <-canceled:
			case <-time.After(2 * time.Second):
				t.Fatal("the upstream request was not canceled")
			}
			var op string
			var cancels []hosttest.Request
			for _, c := range host.Calls() {
				switch c.Method {
				case "host.http.operation_open":
					if c.Request.HostCallbackID != "cb-9" {
						t.Fatalf("operation opened for callback %q", c.Request.HostCallbackID)
					}
				case "host.http.do_stream":
					op = c.Request.OperationID
				case "host.http.cancel":
					cancels = append(cancels, c.Request)
				}
			}
			if len(cancels) == 0 || cancels[0].OperationID != op || cancels[0].HostCallbackID != "cb-9" {
				t.Fatalf("cancel calls %+v for operation %q", cancels, op)
			}
			deadline := time.Now().Add(2 * time.Second)
			for host.Open() != 0 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if n := host.Open(); n != 0 {
				t.Fatalf("%d host operations left open", n)
			}
		})
	}
}

func TestFollowCallerCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	_, err := New(hosttest.New()).Do(ctx, Request{URL: srv.URL})
	if k := kindOf(t, err); k != ErrCanceled {
		t.Fatalf("kind = %s, want canceled", k)
	}
}

func TestFollowResponseCap(t *testing.T) {
	const limit = 1 << 20
	var size atomic.Int64
	var chunked atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := bytes.Repeat([]byte("x"), int(size.Load()))
		if chunked.Load() {
			// Flushing first drops Content-Length: the cap can only be
			// enforced while streaming.
			w.(http.Flusher).Flush()
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	reads := func(h *hosttest.Host) int {
		n := 0
		for _, c := range h.Calls() {
			if c.Method == "host.http.stream_read" {
				n++
			}
		}
		return n
	}
	for _, stream := range []bool{false, true} {
		chunked.Store(stream)

		size.Store(limit)
		host := hosttest.New()
		resp, err := New(host).Do(context.Background(), Request{URL: srv.URL, MaxResponseBytes: limit})
		if err != nil || len(resp.Body) != limit {
			t.Fatalf("chunked=%v: a body of exactly the cap failed: %v", stream, err)
		}

		size.Store(4 * limit)
		host = hosttest.New()
		_, err = New(host).Do(context.Background(), Request{URL: srv.URL, MaxResponseBytes: limit})
		if k := kindOf(t, err); k != ErrTooLarge {
			t.Fatalf("chunked=%v: kind = %s, want too_large", stream, k)
		}
		// Reading stops right after the cap instead of buffering the body.
		if n, most := reads(host), limit/(32<<10)+2; n > most {
			t.Fatalf("chunked=%v: %d stream reads, want at most %d", stream, n, most)
		}
		if n := host.Open(); n != 0 {
			t.Fatalf("chunked=%v: %d host operations left open", stream, n)
		}
	}
}

// hostFunc is a scripted host.
type hostFunc func(method string, req []byte) ([]byte, error)

func (f hostFunc) Call(method string, req []byte) ([]byte, error) { return f(method, req) }

func envelope(ok bool, v any) []byte {
	if !ok {
		b, _ := json.Marshal(map[string]any{"ok": false, "error": map[string]string{"code": "host_call_failed", "message": v.(string)}})
		return b
	}
	res, _ := json.Marshal(v)
	b, _ := json.Marshal(map[string]any{"ok": true, "result": json.RawMessage(res)})
	return b
}

func TestFollowHostFailuresKeepTheDirectClassification(t *testing.T) {
	const u = `"http://site.example.com/api/user/self?secret=abc"`
	cases := map[string]ErrorKind{
		"execute host http request: Get " + u + ": dial tcp: lookup site.example.com on 127.0.0.53:53: no such host":                 ErrDNS,
		"execute host http request: Get " + u + ": dial tcp 192.0.2.1:443: connect: connection refused":                              ErrConnect,
		"execute host http request: Get " + u + ": proxyconnect tcp: dial tcp 127.0.0.1:7890: connect: connection refused":           ErrConnect,
		"execute host http request: Get " + u + ": tls: failed to verify certificate: x509: certificate signed by unknown authority": ErrTLS,
		"execute host http request: Get " + u + ": net/http: TLS handshake timeout":                                                  ErrTimeout,
		"execute host http request: Get " + u + ": read tcp 10.0.0.2:5000->192.0.2.1:443: read: connection reset by peer":            ErrNetwork,
		"host callback ID is not open": ErrNetwork,
	}
	for msg, want := range cases {
		host := hostFunc(func(method string, _ []byte) ([]byte, error) {
			if method == "host.http.operation_open" {
				return envelope(true, map[string]string{"operation_id": "1"}), nil
			}
			if method == "host.http.do_stream" {
				return envelope(false, msg), nil
			}
			return envelope(true, struct{}{}), nil
		})
		_, err := New(host).Do(context.Background(), Request{URL: "http://site.example.com/api/user/self?secret=abc"})
		if k := kindOf(t, err); k != want {
			t.Fatalf("%q: kind = %s, want %s", msg, k, want)
		}
		if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "example.com") {
			t.Fatalf("error echoes the URL: %v", err)
		}
	}
}

func TestFollowRejectsATruncatedStream(t *testing.T) {
	host := hostFunc(func(method string, _ []byte) ([]byte, error) {
		switch method {
		case "host.http.operation_open":
			return envelope(true, map[string]string{"operation_id": "1"}), nil
		case "host.http.do_stream":
			return envelope(true, map[string]any{"status_code": 200, "headers": map[string][]string{"Content-Length": {"10"}}, "stream_id": "s"}), nil
		case "host.http.stream_read":
			return envelope(true, map[string]any{"done": true}), nil
		}
		return envelope(true, struct{}{}), nil
	})
	_, err := New(host).Do(context.Background(), Request{URL: "http://site.example.com/"})
	if k := kindOf(t, err); k != ErrNetwork {
		t.Fatalf("kind = %s, want network", k)
	}
}

func TestFollowWithoutAHostFails(t *testing.T) {
	_, err := New(nil).Do(context.Background(), Request{URL: "http://site.example.com/", Proxy: Follow})
	if k := kindOf(t, err); k != ErrNetwork {
		t.Fatalf("kind = %s, want network", k)
	}
}

func TestCallContextTravelsWithRequest(t *testing.T) {
	ctx := WithCall(context.Background(), Call{HostCallbackID: "cb-1"})
	got, ok := CallFrom(ctx)
	if !ok || got.HostCallbackID != "cb-1" {
		t.Fatalf("call = %+v %v", got, ok)
	}
	if _, ok := CallFrom(context.Background()); ok {
		t.Fatal("call found on a bare context")
	}
}

func TestDirectCallerCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New(nil).Do(ctx, Request{URL: srv.URL, Proxy: Direct})
	if k := kindOf(t, err); k != ErrCanceled {
		t.Fatalf("kind = %s", k)
	}
}

func TestErrorsDoNotEchoURL(t *testing.T) {
	for _, mode := range []Proxy{Direct, Follow} {
		_, err := New(hosttest.New()).Do(context.Background(), Request{URL: "http://newapi-meter-proxy-check.invalid/api/user/self?secret=abc", Proxy: mode})
		if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "invalid/api") {
			t.Fatalf("mode %q: err = %v", mode, err)
		}
		if k := kindOf(t, err); k != ErrDNS {
			t.Fatalf("mode %q: kind = %s, want dns", mode, k)
		}
	}
}
