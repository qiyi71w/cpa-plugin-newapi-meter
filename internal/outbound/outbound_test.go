package outbound

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDirectIgnoresEnvironmentProxy(t *testing.T) {
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

	for _, mode := range []Proxy{Direct, Follow, ""} {
		_, err := New().Do(context.Background(), Request{URL: "http://newapi-meter-proxy-check.invalid/api/status", Proxy: mode})
		var oe *Error
		if !errors.As(err, &oe) || oe.Kind != ErrDNS {
			t.Fatalf("mode %q: err = %v, want a direct DNS failure", mode, err)
		}
	}
	if n := proxied.Load(); n != 0 {
		t.Fatalf("environment proxy received %d requests", n)
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

func TestCallerCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New().Do(ctx, Request{URL: srv.URL})
	var oe *Error
	if !errors.As(err, &oe) || oe.Kind != ErrCanceled {
		t.Fatalf("err = %v", err)
	}
}

func TestErrorsDoNotEchoURL(t *testing.T) {
	_, err := New().Do(context.Background(), Request{URL: "http://newapi-meter-proxy-check.invalid/api/user/self?secret=abc"})
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "invalid/api") {
		t.Fatalf("err = %v", err)
	}
}
