package outbound

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/outbound/hosttest"
)

// Synthetic credentials placed in every position a request can carry them.
const (
	redirHeaderSecret = "synthetic-redirect-header-secret-6101"
	redirQuerySecret  = "synthetic-redirect-query-secret-6102"
	redirBodySecret   = "synthetic-redirect-body-secret-6103"
	redirAuthSecret   = "synthetic-redirect-auth-secret-6104"
)

var redirSecrets = []string{redirHeaderSecret, redirQuerySecret, redirBodySecret, redirAuthSecret}

// origin is a local site that records everything it receives. Two origins
// differ by port, so they are different origins on the same host.
type origin struct {
	*httptest.Server
	mu   sync.Mutex
	seen []string // one dump per request: target, headers, body
}

func newOrigin(t *testing.T, h func(o *origin, w http.ResponseWriter, r *http.Request)) *origin {
	t.Helper()
	o := &origin{}
	o.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(b))
		var d strings.Builder
		d.WriteString(r.Method + " " + r.URL.RequestURI() + "\n")
		_ = r.Header.Write(&d)
		d.Write(b)
		o.mu.Lock()
		o.seen = append(o.seen, d.String())
		o.mu.Unlock()
		h(o, w, r)
	}))
	t.Cleanup(o.Close)
	return o
}

func (o *origin) requests() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.seen...)
}

// leaked reports the synthetic credentials this origin received.
func (o *origin) leaked() []string {
	var out []string
	for _, r := range o.requests() {
		for _, s := range redirSecrets {
			if strings.Contains(r, s) {
				out = append(out, s)
			}
		}
	}
	return out
}

func answerJSON(_ *origin, w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"success":true}`))
}

// redirectTo answers every request with code and a Location built by loc.
func redirectTo(code int, loc func(r *http.Request) string) func(*origin, http.ResponseWriter, *http.Request) {
	return func(_ *origin, w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", loc(r))
		w.WriteHeader(code)
	}
}

func credentialedRequest(u string, proxy Proxy) Request {
	return Request{
		Method: http.MethodPost,
		URL:    u + "?key=" + redirQuerySecret,
		Header: http.Header{
			"Authorization": {"Bearer " + redirAuthSecret},
			"New-Api-User":  {redirHeaderSecret},
			"X-Custom":      {redirHeaderSecret},
		},
		Body:  []byte(`{"secret":"` + redirBodySecret + `"}`),
		Proxy: proxy,
	}
}

func TestDirectCrossOriginRedirectNeverCarriesCredentials(t *testing.T) {
	for _, code := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			b := newOrigin(t, answerJSON)
			a := newOrigin(t, redirectTo(code, func(*http.Request) string { return b.URL + "/landing" }))
			resp, err := New(nil).Do(context.Background(), credentialedRequest(a.URL+"/api/usage", Direct))
			if s := b.leaked(); len(s) != 0 {
				t.Fatalf("the other origin received %v; requests: %q", s, b.requests())
			}
			if err != nil || resp.StatusCode != code {
				t.Fatalf("want the redirect itself back, got %v %v", resp, err)
			}
		})
	}
}

func TestDirectMultiHopRedirectLeavingTheOriginNeverCarriesCredentials(t *testing.T) {
	b := newOrigin(t, answerJSON)
	// A redirects within itself first, then hands off to B.
	a := newOrigin(t, func(o *origin, w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/usage" {
			redirectTo(http.StatusTemporaryRedirect, func(*http.Request) string { return "/api/moved" })(o, w, r)
			return
		}
		redirectTo(http.StatusTemporaryRedirect, func(*http.Request) string { return b.URL + "/landing" })(o, w, r)
	})
	resp, err := New(nil).Do(context.Background(), credentialedRequest(a.URL+"/api/usage", Direct))
	if s := b.leaked(); len(s) != 0 {
		t.Fatalf("the other origin received %v; requests: %q", s, b.requests())
	}
	if err != nil || resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("want the redirect itself back, got %v %v", resp, err)
	}
	if n := len(a.requests()); n != 2 {
		t.Fatalf("own origin got %d requests, want 2 (the same-origin hop is still followed)", n)
	}
}

func TestDirectSameOriginRedirectKeepsTheRequest(t *testing.T) {
	for _, code := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		var got *http.Request
		var body []byte
		a := newOrigin(t, func(o *origin, w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/old" {
				// Relative and absolute same-origin locations are both followed.
				redirectTo(code, func(r *http.Request) string { return "http://" + r.Host + "/new?" + r.URL.RawQuery })(o, w, r)
				return
			}
			got = r.Clone(context.Background())
			body, _ = io.ReadAll(r.Body)
			answerJSON(o, w, r)
		})
		resp, err := New(nil).Do(context.Background(), credentialedRequest(a.URL+"/old", Direct))
		if err != nil || resp.StatusCode != http.StatusOK || string(resp.Body) != `{"success":true}` {
			t.Fatalf("%d: response %v %v", code, resp, err)
		}
		if got.Method != http.MethodPost || got.URL.Query().Get("key") != redirQuerySecret || got.Header.Get("Authorization") != "Bearer "+redirAuthSecret ||
			got.Header.Get("New-Api-User") != redirHeaderSecret || got.Header.Get("X-Custom") != redirHeaderSecret || !strings.Contains(string(body), redirBodySecret) {
			t.Fatalf("%d: the same-origin target lost part of the request: %v %s", code, got.Header, body)
		}
	}
}

func TestDirectRedirectLimitStillApplies(t *testing.T) {
	a := newOrigin(t, redirectTo(http.StatusFound, func(r *http.Request) string { return r.URL.Path + "x" }))
	_, err := New(nil).Do(context.Background(), Request{URL: a.URL + "/loop", Proxy: Direct})
	if k := kindOf(t, err); k != ErrNetwork {
		t.Fatalf("kind = %s, want network", k)
	}
}

// Under follow the host follows every redirect itself (CPA v8.0.78 offers no
// way to stop it); hosttest mirrors its policy. What the plugin guarantees on
// a cross-origin redirect: credential-like headers are dropped by the host,
// and the Referer names only the first origin, so the original URL's query
// does not travel with it. Not covered, by the host's design: headers with
// other names (for example New-Api-User), the body on 307/308, and whatever
// the site itself writes into the Location (such as the original query).
func TestFollowCrossOriginRedirectKeepsCredentialHeadersAndQuery(t *testing.T) {
	for _, tc := range []struct {
		code     int
		multiHop bool
	}{{http.StatusFound, false}, {http.StatusTemporaryRedirect, false}, {http.StatusFound, true}, {http.StatusTemporaryRedirect, true}} {
		code := tc.code
		b := newOrigin(t, answerJSON)
		var aReferer string
		a := newOrigin(t, func(o *origin, w http.ResponseWriter, r *http.Request) {
			aReferer = r.Header.Get("Referer")
			if tc.multiHop && r.URL.Path == "/api/usage" { // a same-origin hop first
				redirectTo(code, func(*http.Request) string { return "/api/moved" })(o, w, r)
				return
			}
			redirectTo(code, func(*http.Request) string { return b.URL + "/landing" })(o, w, r)
		})
		req := Request{
			Method: http.MethodPost,
			URL:    a.URL + "/api/usage?key=" + redirQuerySecret,
			Header: http.Header{"Authorization": {"Bearer " + redirAuthSecret}, "X-Api-Key": {redirHeaderSecret}},
			Proxy:  Follow,
		}
		resp, err := New(hosttest.New()).Do(context.Background(), req)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("%d: %v %v", code, resp, err)
		}
		if s := b.leaked(); len(s) != 0 {
			t.Fatalf("%d: the other origin received %v; requests: %q", code, s, b.requests())
		}
		want := a.URL + "/"
		got := b.requests()
		if len(got) != 1 || !strings.Contains(got[0], "\r\nReferer: "+want+"\r\n") || aReferer != want {
			t.Fatalf("%d: Referer at A %q, requests at B %q; want only the origin %s", code, aReferer, got, want)
		}
	}
}

func TestFollowSameOriginRedirectKeepsTheRequest(t *testing.T) {
	var got *http.Request
	a := newOrigin(t, func(o *origin, w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			redirectTo(http.StatusTemporaryRedirect, func(r *http.Request) string { return "/new?" + r.URL.RawQuery })(o, w, r)
			return
		}
		got = r.Clone(context.Background())
		answerJSON(o, w, r)
	})
	resp, err := New(hosttest.New()).Do(context.Background(), credentialedRequest(a.URL+"/old", Follow))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("%v %v", resp, err)
	}
	if got.Header.Get("Authorization") != "Bearer "+redirAuthSecret || got.Header.Get("X-Custom") != redirHeaderSecret || got.URL.Query().Get("key") != redirQuerySecret {
		t.Fatalf("the same-origin target lost part of the request: %v", got.Header)
	}
}

func TestFollowKeepsAnExplicitReferer(t *testing.T) {
	var got string
	a := newOrigin(t, func(o *origin, w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Referer")
		answerJSON(o, w, r)
	})
	_, err := New(hosttest.New()).Do(context.Background(), Request{URL: a.URL + "/x", Header: http.Header{"Referer": {"https://ui.example.com/"}}})
	if err != nil || got != "https://ui.example.com/" {
		t.Fatalf("Referer %q, %v", got, err)
	}
}

// A script may set an empty Referer. net/http treats an empty explicit value
// as absent and would send the previous hop's full URL; the plugin must send
// the origin-only value instead.
func TestFollowEmptyRefererBecomesTheOrigin(t *testing.T) {
	b := newOrigin(t, answerJSON)
	var aReferer string
	a := newOrigin(t, func(o *origin, w http.ResponseWriter, r *http.Request) {
		aReferer = r.Header.Get("Referer")
		redirectTo(http.StatusFound, func(*http.Request) string { return b.URL + "/landing" })(o, w, r)
	})
	req := Request{URL: a.URL + "/api/usage?key=" + redirQuerySecret, Header: http.Header{"Referer": {""}}, Proxy: Follow}
	if _, err := New(hosttest.New()).Do(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	got := b.requests()
	if s := b.leaked(); len(s) != 0 || len(got) != 1 || !strings.Contains(got[0], "\r\nReferer: "+a.URL+"/\r\n") || aReferer != a.URL+"/" {
		t.Fatalf("Referer at A %q; B got %q (leaked %v); want only the origin %s/", aReferer, got, s, a.URL)
	}
}

// routes answers each URL with a scripted response and records what was
// requested, so redirects across schemes on default ports can be tested
// without binding ports 80 and 443.
type routes struct {
	mu   sync.Mutex
	seen []string
	next map[string]string // URL → Location of a 302; others answer 200
}

func (rt *routes) RoundTrip(r *http.Request) (*http.Response, error) {
	u := r.URL.String()
	rt.mu.Lock()
	rt.seen = append(rt.seen, u)
	rt.mu.Unlock()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}
	if loc, ok := rt.next[u]; ok {
		resp.StatusCode = http.StatusFound
		resp.Header.Set("Location", loc)
	}
	return resp, nil
}

func TestDirectNeverFollowsADowngradeAfterAnUpgrade(t *testing.T) {
	cases := []struct {
		name   string
		next   map[string]string
		status int
		seen   []string
	}{
		{"upgrade then downgrade", map[string]string{
			"http://a.example.com/start": "https://a.example.com/up",
			"https://a.example.com/up":   "http://a.example.com/down",
		}, http.StatusFound, []string{"http://a.example.com/start", "https://a.example.com/up"}},
		{"upgrade, same-origin https hop, then downgrade", map[string]string{
			"http://a.example.com/start": "https://a.example.com/up",
			"https://a.example.com/up":   "https://a.example.com/up2",
			"https://a.example.com/up2":  "http://a.example.com/start2",
		}, http.StatusFound, []string{"http://a.example.com/start", "https://a.example.com/up", "https://a.example.com/up2"}},
		{"upgrade then same-origin https hops", map[string]string{
			"http://a.example.com/start": "https://a.example.com/up",
			"https://a.example.com/up":   "https://A.example.com:443/done",
		}, http.StatusOK, []string{"http://a.example.com/start", "https://a.example.com/up", "https://A.example.com:443/done"}},
	}
	for _, c := range cases {
		rt := &routes{next: c.next}
		cl := New(nil)
		cl.direct.Transport = rt
		resp, err := cl.Do(context.Background(), Request{URL: "http://a.example.com/start", Header: http.Header{"Authorization": {"Bearer " + redirAuthSecret}}, Proxy: Direct})
		if err != nil || resp.StatusCode != c.status || strings.Join(rt.seen, " ") != strings.Join(c.seen, " ") {
			t.Errorf("%s: status %v err %v, requested %v; want %d and %v", c.name, resp, err, rt.seen, c.status, c.seen)
		}
	}
}

func TestDirectFollowsAnIDNHostToItsASCIIForm(t *testing.T) {
	start, _ := url.Parse("http://\u4f8b\u5b50.com/start")
	rt := &routes{next: map[string]string{start.String(): "https://xn--fsqu00a.com/home"}}
	cl := New(nil)
	cl.direct.Transport = rt
	resp, err := cl.Do(context.Background(), Request{URL: start.String(), Header: http.Header{"Authorization": {"Bearer " + redirAuthSecret}}, Proxy: Direct})
	if err != nil || resp.StatusCode != http.StatusOK || len(rt.seen) != 2 || rt.seen[1] != "https://xn--fsqu00a.com/home" {
		t.Fatalf("status %v err %v, requested %v; want the redirect to https://xn--fsqu00a.com/home followed", resp, err, rt.seen)
	}
}

// TestDirectIDNRedirectsFollowOnlyWhenTheSentHostMatches resolves every name
// to one local server and records the Host header net/http writes, so the
// check covers the form actually sent rather than the URL.
func TestDirectIDNRedirectsFollowOnlyWhenTheSentHostMatches(t *testing.T) {
	var mu sync.Mutex
	var hosts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hosts = append(hosts, r.Host+r.URL.Path)
		mu.Unlock()
		if loc := r.URL.Query().Get("to"); loc != "" {
			http.Redirect(w, r, loc, http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	cases := []struct {
		from, to string
		status   int
		hosts    []string
	}{
		{"\u4f8b\u5b50.test", "xn--fsqu00a.test", http.StatusOK, []string{"xn--fsqu00a.test:P/start", "xn--fsqu00a.test:P/done"}},
		{"\u00e9.test", "\u00c9.test", http.StatusFound, []string{"xn--9ca.test:P/start"}}, // É.test is sent as xn--dca.test
		{"k.test", "\u212a.test", http.StatusFound, []string{"k.test:P/start"}},            // K is sent as xn--62g.test
	}
	for _, c := range cases {
		mu.Lock()
		hosts = nil
		mu.Unlock()
		cl := New(nil)
		tr := cl.direct.Transport.(*http.Transport)
		tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
		}
		to := "http://" + c.to + ":" + port + "/done"
		resp, err := cl.Do(context.Background(), Request{URL: "http://" + c.from + ":" + port + "/start?to=" + url.QueryEscape(to), Header: http.Header{"Authorization": {"Bearer " + redirAuthSecret}}, Proxy: Direct})
		if err != nil {
			t.Fatalf("%s -> %s: %v", c.from, c.to, err)
		}
		mu.Lock()
		got := strings.ReplaceAll(strings.Join(hosts, " "), ":"+port+"/", ":P/")
		mu.Unlock()
		if resp.StatusCode != c.status || got != strings.Join(c.hosts, " ") {
			t.Errorf("%s -> %s: status %d, requests %q; want %d and %q", c.from, c.to, resp.StatusCode, got, c.status, c.hosts)
		}
	}
}

func TestSameSite(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{"http://a.example.com/x", "http://a.example.com/y?q=1", true},
		{"http://a.example.com/x", "http://A.EXAMPLE.com:80/y", true},
		{"https://a.example.com/x", "https://a.example.com:443/y", true},
		{"http://a.example.com/x", "https://a.example.com/y", true}, // upgrade to https on the same host
		{"http://a.example.com:8080/x", "https://a.example.com/y", false},
		{"https://a.example.com/x", "http://a.example.com/y", false},
		{"http://a.example.com/x", "http://a.example.com:8080/y", false},
		{"http://a.example.com/x", "http://b.example.com/y", false},
		{"http://a.example.com/x", "http://sub.a.example.com/y", false},
		{"http://example.com/x", "http://a.example.com/y", false},
		// A name is the same host only when both the form the transport
		// dials (IDNA Lookup) and the form net/http writes into Host
		// (httpguts.PunycodeHostPort, no mapping) match, ignoring ASCII case.
		// An IP literal compares as an address, its zone exactly.
		{"http://\u4f8b\u5b50.com/x", "https://xn--fsqu00a.com/y", true}, // 例子.com upgraded to its punycode form
		{"http://xn--fsqu00a.com/x", "http://\u4f8b\u5b50.com/y", true},
		{"http://\u4f8b\u5b50.com/x", "http://XN--FSQU00A.COM/y", true},
		{"http://\u4f8b\u5b50.com/x", "http://\u4f8b.com/y", false},
		{"http://\u4f8b\u5b50.com/x", "http://xn--fsqu00a.com:8080/y", false},
		{"http://\u03c3.example/x", "http://\u03c2.example/y", false}, // σ / ς fold together but are different IDNA hosts
		// KELVIN SIGN dials k.example but is sent as Host: xn--62g.example,
		// so another virtual host on that address would get the request.
		{"http://k.example/x", "http://\u212a.example/y", false},
		// é and É both dial xn--9ca.example but are sent as Host:
		// xn--9ca.example and xn--dca.example.
		{"http://\u00e9.example/x", "http://\u00c9.example/y", false},
		{"http://\u00c9.example/x", "http://xn--9ca.example/y", false},
		{"http://\u00e9.example/x", "http://xn--9ca.example/y", true},
		{"http://\u00e9.example/x", "http://\u00e9.example/y", true},
		{"http://\u00e9.example/x", "http://e.example/y", false},
		{"http://a_b.\u4f8b\u5b50.com/x", "http://a_b.\u4f8b\u5b50.com/y", false}, // no IDNA form: refused even when identical
		{"http://[fe80::1%25eth0]/x", "http://[fe80::1%25ETH0]/y", false},         // zones are case-sensitive
		{"http://[fe80::1%25eth0]/x", "http://[fe80::1%25eth1]/y", false},
		{"http://[fe80::1%25eth0]/x", "http://[fe80::1]/y", false},
		{"http://[fe80::1%25eth0]/x", "http://[fe80::1%25eth0]/y", true},
		{"http://[FE80::1%25eth0]/x", "http://[fe80::1%25eth0]/y", true},
		{"http://[fe80::1%25\u00e9]/x", "http://[fe80::1%25\u00e9]/y", true}, // a non-ASCII zone is still an address
		{"http://[FE80::1%25\u00e9]/x", "http://[fe80:0::1%25\u00e9]/y", true},
		{"http://[fe80::1%25\u00e9]/x", "http://[fe80::1%25\u00c9]/y", false},
		{"http://[FE80::1]/x", "http://[fe80::1]/y", true},
		{"http://[fe80::1]/x", "http://[fe80:0::0:1]/y", true},
		{"http://[fe80::1]/x", "http://[fe80::2]/y", false},
		{"http://127.0.0.1/x", "http://localhost/y", false},
		{"http://[::1]/x", "http://localhost/y", false},
	}
	for _, c := range cases {
		from, err := url.Parse(c.from)
		if err != nil {
			t.Fatal(err)
		}
		to, err := url.Parse(c.to)
		if err != nil {
			t.Fatal(err)
		}
		if got := sameSite(from, to); got != c.want {
			t.Errorf("sameSite(%s, %s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}
