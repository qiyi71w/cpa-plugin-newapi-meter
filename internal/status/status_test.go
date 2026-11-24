package status_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/outbound"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/outbound/hosttest"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/status"
)

// send makes req through the real outbound entry with each transport (direct,
// and follow through a fake host) and classifies the results as the built-in
// source does. Both transports must classify alike.
func send(t *testing.T, ctx func() (context.Context, context.CancelFunc), req outbound.Request) *status.Failure {
	t.Helper()
	var first *status.Failure
	for i, mode := range []outbound.Proxy{outbound.Direct, outbound.Follow} {
		c, cancel := ctx()
		req.Proxy = mode
		resp, err := outbound.New(hosttest.New()).Do(c, req)
		cancel()
		_, f := status.ClassifyNewAPI(resp, err)
		if i == 0 {
			first = f
		} else if !reflect.DeepEqual(f, first) {
			t.Fatalf("follow classified %+v, direct %+v", f, first)
		}
	}
	return first
}

func background() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

// classify sends one request to a handler; see send.
func classify(t *testing.T, h http.HandlerFunc, timeout time.Duration) *status.Failure {
	t.Helper()
	srv := httptest.NewServer(h)
	defer srv.Close()
	return send(t, func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), timeout)
	},
		outbound.Request{URL: srv.URL + "/api/user/self"})
}

func reply(code int, contentType, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}
}

func TestFailureClasses(t *testing.T) {
	cases := []struct {
		name string
		h    http.HandlerFunc
		code status.Code
		diag string
	}{
		{"legacy token retired", reply(401, "application/json", `{"success":false,"message":"legacy access token retired","code":"ACCESS_TOKEN_LEGACY_RETIRED"}`), status.LegacyTokenRetired, "ACCESS_TOKEN_LEGACY_RETIRED"},
		{"PAT scope denied", reply(403, "application/json", `{"success":false,"code":"ACCESS_TOKEN_SCOPE_DENIED"}`), status.PATScopeDenied, "ACCESS_TOKEN_SCOPE_DENIED"},
		{"PAT route undeclared", reply(200, "application/json", `{"success":false,"message":"ACCESS_TOKEN_ROUTE_UNDECLARED"}`), status.PATScopeDenied, "ACCESS_TOKEN_ROUTE_UNDECLARED"},
		{"401", reply(401, "application/json", `{"success":false,"message":"no"}`), status.CredentialInvalid, "HTTP_401"},
		{"403 JSON", reply(403, "application/json", `{"success":false,"message":"forbidden"}`), status.CredentialInvalid, "HTTP_403"},
		{"HTML with 200", reply(200, "text/html; charset=utf-8", "<!DOCTYPE html><html><body>challenge</body></html>"), status.SiteBlocked, "HTML_RESPONSE"},
		{"HTML without content type", reply(403, "", "  <html>blocked</html>"), status.SiteBlocked, "HTML_RESPONSE"},
		{"5xx", reply(502, "text/html", "<html>bad gateway</html>"), status.SiteError, "HTTP_502"},
		{"success false", reply(200, "application/json", `{"success":false,"message":"something else"}`), status.BadResponse, "SUCCESS_FALSE"},
		{"invalid JSON", reply(200, "application/json", `{"success":tru`), status.BadResponse, "INVALID_JSON"},
		{"missing data", reply(200, "application/json", `{"success":true}`), status.BadResponse, "MISSING_DATA"},
		{"404", reply(404, "application/json", `{"message":"not found"}`), status.BadResponse, "HTTP_404"},
		{"plain text", reply(200, "text/plain", `ok`), status.BadResponse, "INVALID_JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := classify(t, tc.h, 5*time.Second)
			if f == nil || f.Code != tc.code || f.Diagnostic != tc.diag {
				t.Fatalf("failure = %+v, want %s/%s", f, tc.code, tc.diag)
			}
		})
	}
}

func TestSuccessIsNotAFailure(t *testing.T) {
	if f := classify(t, reply(200, "application/json", `{"success":true,"data":{"quota":1}}`), 5*time.Second); f != nil {
		t.Fatalf("failure = %+v", f)
	}
	// A successful body that merely mentions an upstream error token is still a success.
	if f := classify(t, reply(200, "application/json", `{"success":true,"data":[{"model_name":"ACCESS_TOKEN_SCOPE_DENIED"}]}`), 5*time.Second); f != nil {
		t.Fatalf("failure = %+v", f)
	}
}

func TestTimeoutIsUnreachable(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	f := classify(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}, 50*time.Millisecond)
	if f == nil || f.Code != status.SiteUnreachable || f.Diagnostic != "NETWORK_TIMEOUT" {
		t.Fatalf("failure = %+v", f)
	}
}

func TestConnectionRefusedIsUnreachable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	f := send(t, background, outbound.Request{URL: "http://" + addr + "/api/status"})
	if f == nil || f.Code != status.SiteUnreachable || f.Diagnostic != "NETWORK_CONNECT" {
		t.Fatalf("failure = %+v", f)
	}
}

func TestTLSFailureIsUnreachable(t *testing.T) {
	srv := httptest.NewTLSServer(reply(200, "application/json", `{"success":true,"data":{}}`))
	defer srv.Close()
	f := send(t, background, outbound.Request{URL: srv.URL})
	if f == nil || f.Code != status.SiteUnreachable || f.Diagnostic != "NETWORK_TLS" {
		t.Fatalf("failure = %+v", f)
	}
}

func TestOversizedResponseIsBadResponse(t *testing.T) {
	srv := httptest.NewServer(reply(200, "application/json", `{"success":true,"data":"`+string(make([]byte, 64))+`"}`))
	defer srv.Close()
	f := send(t, background, outbound.Request{URL: srv.URL, MaxResponseBytes: 16})
	if f == nil || f.Code != status.BadResponse || f.Diagnostic != "RESPONSE_TOO_LARGE" {
		t.Fatalf("failure = %+v", f)
	}
}

func TestDecide(t *testing.T) {
	fail := &status.Failure{Code: status.CredentialInvalid}
	pub := func(f *status.Failure) status.Probe { return status.Probe{Name: "public", Failure: f} }
	cred := func(f *status.Failure) status.Probe {
		return status.Probe{Name: "cred", Credentialed: true, Failure: f}
	}
	five, ten := 5.0, 10.0
	cases := []struct {
		name               string
		probes             []status.Probe
		balance, threshold *float64
		want               status.State
	}{
		{"all ok", []status.Probe{pub(nil), cred(nil)}, &ten, &five, status.OK},
		{"below threshold", []status.Probe{pub(nil), cred(nil)}, &five, &ten, status.Low},
		{"equal to threshold is not low", []status.Probe{cred(nil)}, &five, &five, status.OK},
		{"public failure only", []status.Probe{pub(fail), cred(nil)}, nil, &ten, status.Partial},
		{"one credentialed failure", []status.Probe{cred(fail), cred(nil)}, &five, &ten, status.Partial},
		{"all credentialed fail", []status.Probe{pub(nil), cred(fail), cred(fail)}, nil, nil, status.NeedsAction},
	}
	for _, tc := range cases {
		if got := status.Decide(tc.probes, tc.balance, tc.threshold); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestPrimaryPrefersActionableCredentialedFailure(t *testing.T) {
	probes := []status.Probe{
		{Name: "public", Failure: &status.Failure{Code: status.LegacyTokenRetired}},
		{Name: "a", Credentialed: true, Failure: &status.Failure{Code: status.SiteError}},
		{Name: "b", Credentialed: true, Failure: &status.Failure{Code: status.PATScopeDenied}},
	}
	if got := status.Primary(probes); got == nil || got.Code != status.PATScopeDenied {
		t.Fatalf("primary = %+v", got)
	}
}
