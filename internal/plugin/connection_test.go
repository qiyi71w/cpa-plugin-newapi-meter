package plugin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/store"
)

// answer is one synthetic HTTP response.
type answer struct {
	status int
	ctype  string
	body   string
}

func okData(data any) answer {
	b, _ := json.Marshal(map[string]any{"success": true, "data": data})
	return answer{200, "application/json", string(b)}
}

// testSite is a synthetic new-api site whose answers can be replaced per
// path; it records the credential headers it received.
type testSite struct {
	*httptest.Server
	mu       sync.Mutex
	override map[string]answer
	auth     []string
	userIDs  []string
}

func newTestSite(t *testing.T) *testSite {
	s := &testSite{override: map[string]answer{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		if r.URL.Path != "/api/status" {
			s.auth = append(s.auth, r.Header.Get("Authorization"))
			s.userIDs = append(s.userIDs, r.Header.Get("New-Api-User"))
		}
		a, ok := s.override[r.URL.Path]
		s.mu.Unlock()
		if !ok {
			switch r.URL.Path {
			case "/api/status":
				a = okData(map[string]any{"quota_per_unit": 500000, "quota_display_type": "CNY", "usd_exchange_rate": 7.2, "version": "v0.9.9-test", "enable_data_export": true})
			case "/api/user/self":
				a = okData(map[string]any{"quota": 5000000, "used_quota": 1000000})
			case "/api/log/self/stat":
				a = okData(map[string]any{"quota": 500000, "rpm": 2, "tpm": 900})
			case "/api/data/self":
				a = okData([]any{})
			default:
				a = answer{404, "text/plain", "not found"}
			}
		}
		w.Header().Set("Content-Type", a.ctype)
		w.WriteHeader(a.status)
		_, _ = w.Write([]byte(a.body))
	}))
	t.Cleanup(s.Close)
	return s
}

// credentialed sets the same answer on every endpoint that needs the credential.
func (s *testSite) credentialed(a answer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range []string{"/api/user/self", "/api/log/self/stat", "/api/data/self"} {
		s.override[p] = a
	}
}

func (s *testSite) set(path string, a answer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.override[path] = a
}

func (s *testSite) seen() (auth, users []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.auth...), append([]string(nil), s.userIDs...)
}

func (h *harness) testConnection(body map[string]any) (int, map[string]any) {
	h.t.Helper()
	r := h.do("POST", RoutePrefix+"/accounts/test", nil, body)
	assertNoSecrets(h.t, "test response", string(r.Body))
	return r.Status, r.json(h.t)
}

// snapshot captures the data file so a test can prove it was not written.
type snapshot struct {
	exists bool
	bytes  []byte
	mod    time.Time
}

func (h *harness) snapshot() snapshot {
	path := filepath.Join(h.dir, store.FileName)
	info, err := os.Stat(path)
	if err != nil {
		return snapshot{}
	}
	b, _ := os.ReadFile(path)
	return snapshot{true, b, info.ModTime()}
}

func (h *harness) assertUnchanged(before snapshot) {
	h.t.Helper()
	after := h.snapshot()
	if after.exists != before.exists || !bytes.Equal(after.bytes, before.bytes) || !after.mod.Equal(before.mod) {
		h.t.Fatal("connection test wrote the data file")
	}
	entries, _ := os.ReadDir(h.dir)
	for _, e := range entries {
		if e.Name() != store.FileName {
			h.t.Fatalf("connection test left %s in the data directory", e.Name())
		}
	}
}

func TestConnectionTestSuccessReportsSiteWithoutSaving(t *testing.T) {
	h := newHarness(t)
	site := newTestSite(t)
	before := h.snapshot()
	// No name: a form can be tried before it is complete.
	st, res := h.testConnection(map[string]any{"base_url": site.URL + "/", "credential": secretPAT, "proxy": "direct"})
	if st != 200 || res["ok"] != true || res["state"] != "ok" || res["source"] != "newapi" {
		t.Fatalf("test = %d %v", st, res)
	}
	cur := res["currency"].(map[string]any)
	if res["credential_kind"] != "pat" || res["site_version"] != "v0.9.9-test" || cur["code"] != "CNY" || cur["symbol"] != "¥" {
		t.Fatalf("test = %v", res)
	}
	if res["failure"] != nil || len(res["failures"].([]any)) != 0 {
		t.Fatalf("failures = %v", res["failures"])
	}
	auth, users := site.seen()
	for i := range auth {
		if auth[i] != "Bearer "+secretPAT || users[i] != "" {
			t.Fatalf("PAT test sent %q / New-Api-User %q", auth[i], users[i])
		}
	}
	h.assertUnchanged(before)
	if n := len(h.do("GET", RoutePrefix+"/accounts", nil, nil).json(t)["accounts"].([]any)); n != 0 {
		t.Fatalf("a connection test created %d accounts", n)
	}
}

func TestConnectionTestClassifiesEveryFailure(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()

	jsonErr := func(status int, code string) answer {
		b, _ := json.Marshal(map[string]any{"success": false, "message": "synthetic refusal", "code": code})
		return answer{status, "application/json", string(b)}
	}
	cases := []struct {
		name       string
		credential string
		setup      func(s *testSite)
		baseURL    string
		code       string
		diagnostic string
		state      string
	}{
		{"unreachable", secretPAT, nil, closedURL, "site_unreachable", "", "needs_action"},
		{"legacy retired", secretLegacy, func(s *testSite) { s.credentialed(jsonErr(401, "ACCESS_TOKEN_LEGACY_RETIRED")) }, "", "legacy_token_retired", "ACCESS_TOKEN_LEGACY_RETIRED", "needs_action"},
		{"scope denied", secretPAT, func(s *testSite) { s.credentialed(jsonErr(403, "ACCESS_TOKEN_SCOPE_DENIED")) }, "", "pat_scope_denied", "ACCESS_TOKEN_SCOPE_DENIED", "needs_action"},
		{"route undeclared", secretPAT, func(s *testSite) { s.credentialed(jsonErr(403, "ACCESS_TOKEN_ROUTE_UNDECLARED")) }, "", "pat_scope_denied", "ACCESS_TOKEN_ROUTE_UNDECLARED", "needs_action"},
		{"credential invalid", secretLegacy, func(s *testSite) { s.credentialed(jsonErr(401, "")) }, "", "credential_invalid", "HTTP_401", "needs_action"},
		// A WAF challenge on the balance endpoint only: the rest still reads.
		{"site blocked", secretPAT, func(s *testSite) {
			s.set("/api/user/self", answer{200, "text/html; charset=utf-8", "<!doctype html><title>verify</title>"})
		}, "", "site_blocked", "HTML_RESPONSE", "partial"},
		{"site error", secretPAT, func(s *testSite) { s.credentialed(answer{502, "text/html", "<html>bad gateway</html>"}) }, "", "site_error", "HTTP_502", "needs_action"},
		{"bad response", secretPAT, func(s *testSite) { s.credentialed(answer{200, "application/json", "not json"}) }, "", "bad_response", "INVALID_JSON", "needs_action"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			site := newTestSite(t)
			if c.setup != nil {
				c.setup(site)
			}
			base := c.baseURL
			if base == "" {
				base = site.URL
			}
			before := h.snapshot()
			st, res := h.testConnection(map[string]any{"base_url": base, "credential": c.credential, "user_id": "42"})
			if st != 200 || res["ok"] != false || res["state"] != c.state {
				t.Fatalf("test = %d %v", st, res)
			}
			f := res["failure"].(map[string]any)
			if f["code"] != c.code || (c.diagnostic != "" && f["diagnostic"] != c.diagnostic) {
				t.Fatalf("failure = %v, want %s/%s", f, c.code, c.diagnostic)
			}
			if len(res["failures"].([]any)) == 0 {
				t.Fatal("failed queries are not listed")
			}
			h.assertUnchanged(before)
		})
	}
}

func TestConnectionTestValidatesLikeSave(t *testing.T) {
	h := newHarness(t)
	site := newTestSite(t)
	for _, c := range []struct {
		body          map[string]any
		field, reason string
	}{
		{map[string]any{"base_url": site.URL, "credential": secretLegacy}, "user_id", "required"},
		{map[string]any{"base_url": site.URL, "credential": secretLegacy, "user_id": "4x"}, "user_id", "not_numeric"},
		{map[string]any{"base_url": "ftp://newapi.example.com", "credential": secretPAT}, "base_url", "scheme"},
		{map[string]any{"base_url": site.URL, "credential": ""}, "credential", "required"},
		{map[string]any{"base_url": site.URL, "credential": secretPAT, "source": "custom"}, "custom_script", "required"},
		{map[string]any{"source": "custom", "custom_script": "({})", "custom_placeholders": map[string]any{"baseUrl": "ftp://api.example.com"}}, "custom_placeholders.baseUrl", "scheme"},
	} {
		st, res := h.testConnection(c.body)
		e, _ := res["error"].(map[string]any)
		if st != 400 || e["code"] != "validation_failed" || e["field"] != c.field || e["reason"] != c.reason {
			t.Fatalf("%v → %d %v, want %s/%s", c.body, st, res, c.field, c.reason)
		}
	}
	if auth, _ := site.seen(); len(auth) != 0 {
		t.Fatal("an invalid form reached the site")
	}
	if st, res := h.testConnection(map[string]any{"id": "acc_missing", "base_url": site.URL}); st != 404 || res["error"].(map[string]any)["code"] != "account_not_found" {
		t.Fatalf("unknown id → %d %v", st, res)
	}
}

func TestConnectionTestOnEditUsesStoredCredential(t *testing.T) {
	h := newHarness(t)
	site := newTestSite(t)
	r := h.do("POST", RoutePrefix+"/accounts", nil, map[string]any{"name": "Legacy", "base_url": site.URL, "credential": secretLegacy, "user_id": "42"})
	if r.Status != 201 {
		t.Fatalf("create: %d %s", r.Status, r.Body)
	}
	acc := r.json(t)["account"].(map[string]any)
	id, masked := acc["id"].(string), acc["credential_masked"].(string)
	before := h.snapshot()

	// Blank and masked both mean the stored credential.
	for _, cred := range []string{"", masked} {
		st, res := h.testConnection(map[string]any{"id": id, "base_url": site.URL, "credential": cred, "user_id": "42"})
		if st != 200 || res["ok"] != true || res["credential_kind"] != "legacy" {
			t.Fatalf("credential %q: %d %v", cred, st, res)
		}
	}
	auth, users := site.seen()
	if len(auth) == 0 {
		t.Fatal("the site was not queried")
	}
	for i := range auth {
		if auth[i] != "Bearer "+secretLegacy || users[i] != "42" {
			t.Fatalf("edit test sent %q / New-Api-User %q", auth[i], users[i])
		}
	}

	// A newly typed credential replaces the stored one for the test only.
	st, res := h.testConnection(map[string]any{"id": id, "base_url": site.URL, "credential": secretPAT})
	if st != 200 || res["credential_kind"] != "pat" {
		t.Fatalf("new credential: %d %v", st, res)
	}
	if auth, _ := site.seen(); auth[len(auth)-1] != "Bearer "+secretPAT {
		t.Fatalf("new credential not used: %q", auth[len(auth)-1])
	}
	h.assertUnchanged(before)
	logs := h.logs.String()
	assertNoSecrets(t, "plugin log", logs)
}
