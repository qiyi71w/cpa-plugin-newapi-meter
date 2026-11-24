package plugin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/outbound"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/outbound/hosttest"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/store"
)

const (
	secretPAT    = "nap_synthetic_personal_access_token_9001"
	secretLegacy = "synthetic-legacy-access-token-9002"
	managementKy = "synthetic-management-key-9003"
)

// upstream is a synthetic new-api site that counts requests per endpoint.
// When echo is set, every endpoint fails and echoes the Authorization header
// back in its body, so tests can prove response bodies never leak.
type upstream struct {
	*httptest.Server
	hits    sync.Map // path → *atomic.Int32
	echo    atomic.Bool
	delay   atomic.Int64
	reached chan struct{}
}

func newUpstream(t *testing.T) *upstream {
	u := &upstream{reached: make(chan struct{}, 64)}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := u.hits.LoadOrStore(r.URL.Path, new(atomic.Int32))
		n.(*atomic.Int32).Add(1)
		select {
		case u.reached <- struct{}{}:
		default:
		}
		if d := u.delay.Load(); d > 0 {
			time.Sleep(time.Duration(d))
		}
		w.Header().Set("Content-Type", "application/json")
		if u.echo.Load() && r.URL.Path != "/api/status" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"success":false,"message":"bad token ` + r.Header.Get("Authorization") + `"}`))
			return
		}
		var data any
		switch r.URL.Path {
		case "/api/status":
			data = map[string]any{"quota_per_unit": 500000, "quota_display_type": "USD", "version": "v0.0.0-test", "enable_data_export": true}
		case "/api/user/self":
			data = map[string]any{"quota": 5000000, "used_quota": 1000000}
		case "/api/log/self/stat":
			data = map[string]any{"quota": 500000, "rpm": 2, "tpm": 900}
		case "/api/data/self":
			data = []any{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *upstream) count(path string) int32 {
	n, ok := u.hits.Load(path)
	if !ok {
		return 0
	}
	return n.(*atomic.Int32).Load()
}

type harness struct {
	t     *testing.T
	p     *Plugin
	host  *hosttest.Host
	logs  *syncBuffer
	clock *clock
	dir   string
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// newHarness builds a plugin whose follow transport goes through a fake host,
// so accounts with the default proxy option reach the synthetic sites too.
func newHarness(t *testing.T) *harness {
	h := &harness{t: t, host: hosttest.New(), logs: &syncBuffer{}, clock: &clock{t: time.Date(2026, 5, 2, 9, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))}}
	h.dir = filepath.Join(t.TempDir(), "data")
	h.p = New(Options{Now: h.clock.now, Log: h.logs, HTTP: outbound.New(h.host), Page: []byte("<!doctype html><html><head><style>b{}</style></head><body><script>1</script></body></html>")})
	h.register()
	return h
}

func (h *harness) register() map[string]any {
	cfg := base64.StdEncoding.EncodeToString([]byte("enabled: true\npriority: 0\ndata_dir: " + h.dir + "\n"))
	return h.call("plugin.register", `{"config_yaml":"`+cfg+`","schema_version":10}`)
}

func (h *harness) call(method, raw string) map[string]any {
	h.t.Helper()
	var env struct {
		OK     bool           `json:"ok"`
		Result map[string]any `json:"result"`
	}
	out := h.p.Call(method, []byte(raw))
	if err := json.Unmarshal(out, &env); err != nil || !env.OK {
		h.t.Fatalf("%s: %s", method, out)
	}
	return env.Result
}

type reply struct {
	Status  int
	Headers http.Header
	Body    []byte
}

func (h *harness) do(method, path string, query url.Values, body any) reply {
	h.t.Helper()
	req := map[string]any{"Method": method, "Path": path, "Query": query, "Headers": map[string][]string{"Authorization": {"Bearer " + managementKy}}, "host_callback_id": "cb-test"}
	if body != nil {
		b, _ := json.Marshal(body)
		req["Body"] = b
	}
	raw, _ := json.Marshal(req)
	var env struct {
		OK     bool `json:"ok"`
		Result struct {
			StatusCode int
			Headers    http.Header
			Body       []byte
		} `json:"result"`
	}
	if err := json.Unmarshal(h.p.Call("management.handle", raw), &env); err != nil || !env.OK {
		h.t.Fatalf("management.handle failed")
	}
	return reply{env.Result.StatusCode, env.Result.Headers, env.Result.Body}
}

func (r reply) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body, &m); err != nil {
		t.Fatalf("body %q: %v", r.Body, err)
	}
	return m
}

func (h *harness) create(u *upstream, name, cred string, threshold *float64) string {
	h.t.Helper()
	in := map[string]any{"name": name, "base_url": u.URL + "/", "credential": cred, "low_balance_threshold": threshold}
	if !strings.HasPrefix(cred, "nap_") {
		in["user_id"] = "42"
	}
	r := h.do("POST", RoutePrefix+"/accounts", nil, in)
	if r.Status != http.StatusCreated {
		h.t.Fatalf("create: %d %s", r.Status, r.Body)
	}
	return r.json(h.t)["account"].(map[string]any)["id"].(string)
}

func (h *harness) overview(q url.Values) []map[string]any {
	h.t.Helper()
	r := h.do("GET", RoutePrefix+"/overview", q, nil)
	if r.Status != 200 {
		h.t.Fatalf("overview: %d %s", r.Status, r.Body)
	}
	var out []map[string]any
	for _, a := range r.json(h.t)["accounts"].([]any) {
		out = append(out, a.(map[string]any))
	}
	return out
}

func assertNoSecrets(t *testing.T, where string, text string) {
	t.Helper()
	for _, s := range []string{secretPAT, secretLegacy, managementKy} {
		if strings.Contains(text, s) {
			t.Fatalf("%s contains a full secret", where)
		}
	}
}

func ptr(v float64) *float64 { return &v }

func TestRegistrationDeclaresOnlyManagementAPI(t *testing.T) {
	h := newHarness(t)
	reg := h.register()
	if reg["schema_version"] != float64(6) {
		t.Fatalf("schema_version = %v", reg["schema_version"])
	}
	caps := reg["capabilities"].(map[string]any)
	if len(caps) != 1 || caps["management_api"] != true {
		t.Fatalf("capabilities = %v", caps)
	}
	fields := reg["metadata"].(map[string]any)["ConfigFields"].([]any)
	if len(fields) != 1 || fields[0].(map[string]any)["Name"] != "data_dir" {
		t.Fatalf("config fields = %v", fields)
	}
	mr := h.call("management.register", `{"BasePath":"/v0/management","ResourceBasePath":"/v0/resource/plugins/newapi-meter"}`)
	res := mr["resources"].([]any)
	if len(res) != 1 || res[0].(map[string]any)["Menu"] != MenuName || res[0].(map[string]any)["Path"] != PagePath {
		t.Fatalf("resources = %v", res)
	}
	if len(mr["routes"].([]any)) != len(Routes) {
		t.Fatalf("routes = %v", mr["routes"])
	}
}

func TestResourceRouteServesOnlyThePage(t *testing.T) {
	h := newHarness(t)
	u := newUpstream(t)
	h.create(u, "A", secretPAT, nil)
	h.overview(nil)
	r := h.do("GET", PagePath, url.Values{"force": {"1"}, "id": {"x"}}, nil)
	if r.Status != 200 || r.Headers.Get("Content-Type") != "text/html; charset=utf-8" || !bytes.Equal(r.Body, h.p.page) {
		t.Fatalf("page: %d %v %q", r.Status, r.Headers, r.Body)
	}
	csp := r.Headers.Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'sha256-") || !strings.Contains(csp, "connect-src 'self'") || !strings.Contains(csp, "frame-ancestors 'self'") {
		t.Fatalf("csp = %q", csp)
	}
	if h.do("POST", PagePath, nil, nil).Status != http.StatusMethodNotAllowed {
		t.Fatal("non-GET on the page route was served")
	}
	if h.do("GET", "/v0/resource/plugins/newapi-meter/accounts", nil, nil).Status != http.StatusNotFound {
		t.Fatal("other resource paths are served")
	}
}

func TestAccountRoutesMaskCredentials(t *testing.T) {
	h := newHarness(t)
	u := newUpstream(t)
	id := h.create(u, "Legacy", secretLegacy, ptr(2))

	list := h.do("GET", RoutePrefix+"/accounts", nil, nil)
	assertNoSecrets(t, "list response", string(list.Body))
	acc := list.json(t)["accounts"].([]any)[0].(map[string]any)
	if acc["credential_masked"] == "" || acc["credential_kind"] != "legacy" || acc["base_url"] != u.URL || acc["proxy"] != "follow" {
		t.Fatalf("account view = %v", acc)
	}
	if _, leaked := acc["credential"]; leaked {
		t.Fatal("list returns a credential field")
	}

	// Blank credential keeps the stored one.
	upd := h.do("PUT", RoutePrefix+"/accounts", nil, map[string]any{"id": id, "name": "Renamed", "base_url": u.URL, "user_id": "42", "low_balance_threshold": 3})
	if upd.Status != 200 {
		t.Fatalf("update: %d %s", upd.Status, upd.Body)
	}
	assertNoSecrets(t, "update response", string(upd.Body))
	raw, _ := os.ReadFile(filepath.Join(h.dir, store.FileName))
	if !strings.Contains(string(raw), secretLegacy) || !strings.Contains(string(raw), "Renamed") {
		t.Fatal("update lost the credential or the new name")
	}

	bad := h.do("POST", RoutePrefix+"/accounts", nil, map[string]any{"name": "x", "base_url": "https://x.example.com", "credential": "legacy-without-user"})
	if bad.Status != 400 || bad.json(t)["error"].(map[string]any)["field"] != "user_id" {
		t.Fatalf("validation: %d %s", bad.Status, bad.Body)
	}
	if h.do("DELETE", RoutePrefix+"/accounts", url.Values{"id": {"acc_missing"}}, nil).Status != 404 {
		t.Fatal("delete of a missing account did not 404")
	}
	if r := h.do("DELETE", RoutePrefix+"/accounts", url.Values{"id": {id}}, nil); r.Status != 200 {
		t.Fatalf("delete: %d %s", r.Status, r.Body)
	}
	if n := len(h.do("GET", RoutePrefix+"/accounts", nil, nil).json(t)["accounts"].([]any)); n != 0 {
		t.Fatalf("accounts after delete = %d", n)
	}
}

func TestOverviewAndLogsNeverContainSecrets(t *testing.T) {
	h := newHarness(t)
	ok := newUpstream(t)
	echo := newUpstream(t)
	echo.echo.Store(true)
	h.create(ok, "Fine", secretPAT, ptr(100))
	h.create(echo, "Echo", secretLegacy, nil)
	r := h.do("GET", RoutePrefix+"/overview", nil, nil)
	assertNoSecrets(t, "overview response", string(r.Body))
	items := h.overview(nil)
	if items[0]["state"] != "low" || items[0]["balance"] != float64(10) || items[0]["today_spend"] != float64(1) || items[0]["host"] == "" {
		t.Fatalf("fine account = %v", items[0])
	}
	if items[1]["state"] != "needs_action" || items[1]["primary_failure"].(map[string]any)["code"] != "credential_invalid" {
		t.Fatalf("echo account = %v", items[1])
	}
	logs := h.logs.String()
	if !strings.Contains(logs, "site query failed") {
		t.Fatalf("expected failure logs, got %q", logs)
	}
	assertNoSecrets(t, "plugin log", logs)
	if strings.Contains(logs, "bad token") {
		t.Fatal("plugin log contains an upstream response body")
	}
}

func TestOverviewCacheForceAndCoalescing(t *testing.T) {
	h := newHarness(t)
	u := newUpstream(t)
	id := h.create(u, "A", secretPAT, nil)

	first := h.overview(nil)
	if first[0]["cached"] != false || u.count("/api/user/self") != 1 {
		t.Fatalf("first fetch: cached=%v hits=%d", first[0]["cached"], u.count("/api/user/self"))
	}
	h.clock.add(30 * time.Second)
	if second := h.overview(nil); second[0]["cached"] != true || u.count("/api/user/self") != 1 {
		t.Fatalf("within 60 s the site was asked again")
	}
	h.overview(url.Values{"force": {"1"}})
	if u.count("/api/user/self") != 2 {
		t.Fatalf("force did not bypass the cache")
	}
	h.clock.add(61 * time.Second)
	h.overview(nil)
	if u.count("/api/user/self") != 3 {
		t.Fatalf("expired entry was served")
	}
	// Forcing another account id leaves this one cached.
	h.overview(url.Values{"force": {"1"}, "id": {"acc_other"}})
	if u.count("/api/user/self") != 3 {
		t.Fatalf("force for another id refetched this account")
	}
	h.overview(url.Values{"force": {"1"}, "id": {id}})
	if u.count("/api/user/self") != 4 {
		t.Fatalf("force for this id did not refetch")
	}

	// Concurrent overview calls while a fetch is running share it.
	u.delay.Store(int64(100 * time.Millisecond))
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q := url.Values{}
			if i%2 == 0 {
				q.Set("force", "1")
			}
			h.overview(q)
		}()
	}
	wg.Wait()
	if got := u.count("/api/user/self"); got != 5 {
		t.Fatalf("concurrent overviews fetched %d times, want one more (5)", got)
	}
}

func TestLastSuccessSurvivesLaterFailure(t *testing.T) {
	h := newHarness(t)
	u := newUpstream(t)
	h.create(u, "A", secretPAT, nil)
	first := h.overview(nil)
	okAt := first[0]["last_success_at"]
	if okAt == nil {
		t.Fatal("no last success after a successful fetch")
	}
	u.echo.Store(true)
	h.clock.add(2 * time.Minute)
	later := h.overview(nil)
	if later[0]["state"] != "needs_action" || later[0]["last_success_at"] != okAt {
		t.Fatalf("later = %v", later[0])
	}
}

func TestUnreadableDataFileIsReportedAndKept(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(h.dir, store.FileName)
	content := []byte(`{"schema_version":99,"accounts":[]}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, r := range []reply{
		h.do("GET", RoutePrefix+"/overview", nil, nil),
		h.do("GET", RoutePrefix+"/accounts", nil, nil),
		h.do("POST", RoutePrefix+"/accounts", nil, map[string]any{"name": "x", "base_url": "https://x.example.com", "credential": secretPAT}),
	} {
		e := r.json(t)["error"].(map[string]any)
		if r.Status != http.StatusServiceUnavailable || e["code"] != "data_file_newer" || e["path"] != path {
			t.Fatalf("response = %d %s", r.Status, r.Body)
		}
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, content) {
		t.Fatal("refused file was rewritten")
	}
}

func TestEmptyOverviewAndUnknownRoutes(t *testing.T) {
	h := newHarness(t)
	if items := h.overview(nil); len(items) != 0 {
		t.Fatalf("items = %v", items)
	}
	body := h.do("GET", RoutePrefix+"/overview", nil, nil).json(t)
	if body["timezone"] != "UTC+8" || body["today_start"] != "2026-05-02T00:00:00+08:00" {
		t.Fatalf("overview meta = %v", body)
	}
	if h.do("GET", RoutePrefix+"/nope", nil, nil).Status != 404 {
		t.Fatal("unknown route did not 404")
	}
	out := h.p.Call("executor.execute", []byte(`{}`))
	if !bytes.Contains(out, []byte(`"ok":false`)) {
		t.Fatalf("unsupported method answered %s", out)
	}
}

func TestRelativeDataDirIsRejected(t *testing.T) {
	h := newHarness(t)
	cfg := base64.StdEncoding.EncodeToString([]byte("data_dir: relative/dir\n"))
	h.call("plugin.reconfigure", `{"config_yaml":"`+cfg+`"}`)
	r := h.do("GET", RoutePrefix+"/accounts", nil, nil)
	if r.Status != http.StatusServiceUnavailable || r.json(t)["error"].(map[string]any)["code"] != "data_file_unavailable" {
		t.Fatalf("response = %d %s", r.Status, r.Body)
	}
}
