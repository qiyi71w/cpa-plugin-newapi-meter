package newapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/outbound"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/outbound/hosttest"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/status"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/store"
)

const (
	redirLegacy = "synthetic-redirect-legacy-token-6301"
	redirUserID = "630263026302"
)

// A site whose credentialed endpoints redirect to another origin: the other
// origin never sees the access token or the user ID, and the endpoints fail
// with the existing classes instead of reading the other origin's data.
func TestCrossOriginRedirectKeepsTheCredentialOnTheSite(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var d strings.Builder
		d.WriteString(r.URL.RequestURI() + "\n")
		_ = r.Header.Write(&d)
		mu.Lock()
		seen = append(seen, d.String())
		mu.Unlock()
		ok(w, map[string]any{"quota": 1, "used_quota": 1, "rpm": 0, "tpm": 0})
	}))
	t.Cleanup(other.Close)

	f := newFakeSite(t)
	for _, p := range []string{EndpointSelf, EndpointStat, EndpointData} {
		f.on(p, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", other.URL+r.URL.RequestURI())
			w.WriteHeader(http.StatusFound)
		})
	}
	a := account(f, testPAT)
	a.Credential, a.UserID = redirLegacy, redirUserID
	r := source(fixedNow).Fetch(context.Background(), a)

	mu.Lock()
	got := strings.Join(seen, "\n")
	mu.Unlock()
	for _, v := range []string{redirLegacy, redirUserID} {
		if strings.Contains(got, v) {
			t.Fatalf("the other origin received %q:\n%s", v, got)
		}
	}
	for _, p := range r.Probes[1:] {
		if p.Failure == nil || p.Failure.Code != status.BadResponse || p.Failure.Diagnostic != "HTTP_302" {
			t.Fatalf("%s: failure %+v, want bad_response HTTP_302", p.Name, p.Failure)
		}
	}
}

// Redirects that stay on the site are followed with the credential.
func TestSameOriginRedirectIsFollowed(t *testing.T) {
	f := newFakeSite(t)
	f.on(EndpointSelf, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("moved") == "" {
			http.Redirect(w, r, EndpointSelf+"?moved=1", http.StatusMovedPermanently)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+testLegacy || r.Header.Get("New-Api-User") != "42" {
			unauthorized(w, r)
			return
		}
		ok(w, map[string]any{"quota": 5000000, "used_quota": 2500000})
	})
	_, s := fetch(t, f, testLegacy, nil)
	if s.State != status.OK {
		t.Fatalf("state = %s, failures %+v", s.State, s.Failures)
	}
	near(t, "balance", s.Balance, 10)
}

// Under follow, CPA's host follows the redirect itself and drops the
// Authorization header on the way to another origin; the plugin's Referer
// names only the site. The user ID header (New-Api-User) is not a
// credential-like name to the host and still reaches the other origin.
func TestFollowCrossOriginRedirectKeepsTheTokenOnTheSite(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var d strings.Builder
		d.WriteString(r.URL.RequestURI() + "\n")
		_ = r.Header.Write(&d)
		mu.Lock()
		seen = append(seen, d.String())
		mu.Unlock()
		ok(w, map[string]any{"quota": 1, "used_quota": 1})
	}))
	t.Cleanup(other.Close)
	f := newFakeSite(t)
	f.on(EndpointSelf, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.RequestURI(), http.StatusFound)
	})
	a := account(f, testPAT)
	a.Credential, a.UserID, a.Proxy = redirLegacy, redirUserID, store.ProxyFollow
	src := source(fixedNow)
	src.HTTP = outbound.New(hosttest.New())
	src.Fetch(context.Background(), a)

	mu.Lock()
	got := strings.Join(seen, "\n")
	mu.Unlock()
	if got == "" {
		t.Fatal("the host did not follow the redirect")
	}
	if strings.Contains(got, redirLegacy) {
		t.Fatalf("the other origin received the access token:\n%s", got)
	}
	if !strings.Contains(got, "\r\nReferer: "+f.URL+"/\r\n") {
		t.Fatalf("Referer is not the site's origin:\n%s", got)
	}
}
