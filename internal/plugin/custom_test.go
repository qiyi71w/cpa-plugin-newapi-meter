package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/newapi"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/outbound"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/store"
)

const (
	customKey    = "sk-synthetic-custom-route-key-7001"
	customToken  = "synthetic-custom-route-token-7002"
	customUserID = "user-7003"
)

// customValues are every placeholder value an account below uses; none may
// ever reach the plugin log, and the secret ones no route response.
func customValues(base string, h *harness) []string {
	now := h.clock.now()
	return []string{customKey, customToken, customUserID, base,
		strconv.FormatInt(now.Unix(), 10), strconv.FormatInt(newapi.LocalMidnight(now).Unix(), 10)}
}

func assertNoCustomSecrets(t *testing.T, where, text string) {
	t.Helper()
	for _, s := range []string{customKey, customToken} {
		if strings.Contains(text, s) {
			t.Fatalf("%s contains a full apiKey/accessToken", where)
		}
	}
}

// customSite answers every path with the current body and records the
// Authorization header and the request count.
type customSite struct {
	*httptest.Server
	body atomic.Value
	auth atomic.Value
	hits atomic.Int32
}

func newCustomSite(t *testing.T, body string) *customSite {
	s := &customSite{}
	s.body.Store(body)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		s.auth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(s.body.Load().(string)))
	}))
	t.Cleanup(s.Close)
	return s
}

const plansScript = `({
  request: {
    url: "{{baseUrl}}/v1/plans?user={{userId}}&from={{todayStart}}",
    headers: { "Authorization": "Bearer {{apiKey}}", "X-Token": "{{accessToken}}" }
  },
  extractor: function (r) {
    if (!r.ok) return { isValid: false, invalidMessage: r.reason };
    return r.plans.map(function (p) {
      return { planName: p.name, remaining: p.left, used: p.used, total: p.total, unit: p.unit, todaySpend: p.today };
    });
  }
})`

func customInput(name, base string, threshold *float64) map[string]any {
	return map[string]any{
		"name": name, "source": "custom", "custom_script": plansScript, "low_balance_threshold": threshold, "proxy": "direct",
		"custom_placeholders": map[string]any{"baseUrl": base + "/", "apiKey": customKey, "accessToken": customToken, "userId": customUserID},
	}
}

func (h *harness) createCustom(name, base string, threshold *float64) map[string]any {
	h.t.Helper()
	r := h.do("POST", RoutePrefix+"/accounts", nil, customInput(name, base, threshold))
	if r.Status != http.StatusCreated {
		h.t.Fatalf("create: %d %s", r.Status, r.Body)
	}
	assertNoCustomSecrets(h.t, "create response", string(r.Body))
	return r.json(h.t)["account"].(map[string]any)
}

func TestCustomAccountOnOverview(t *testing.T) {
	h := newHarness(t)
	sum := newCustomSite(t, `{"ok": true, "plans": [
		{"name": "monthly", "left": 3, "used": 7, "total": 10, "unit": "USD", "today": 0.5},
		{"name": "top-up", "left": 1.5, "unit": "USD", "today": 9}]}`)
	first := newCustomSite(t, `{"ok": true, "plans": [
		{"name": "credits", "left": 120, "unit": "credits"},
		{"name": "cash", "left": 4, "unit": "CNY"}]}`)
	invalid := newCustomSite(t, `{"ok": false, "reason": "synthetic: key disabled"}`)
	u := newUpstream(t)
	h.create(u, "Built-in", secretPAT, nil)

	acc := h.createCustom("Sum", sum.URL, ptr(5))
	ph := acc["custom_placeholders"].(map[string]any)
	if acc["source"] != "custom" || acc["base_url"] != sum.URL || ph["baseUrl"] != sum.URL || ph["userId"] != customUserID ||
		ph["apiKeyMasked"] != store.Mask(customKey) || ph["accessTokenMasked"] != store.Mask(customToken) || acc["custom_script"] != plansScript {
		t.Fatalf("account view = %v", acc)
	}
	h.createCustom("First", first.URL, nil)
	h.createCustom("Invalid", invalid.URL, nil)

	r := h.do("GET", RoutePrefix+"/overview", nil, nil)
	assertNoCustomSecrets(t, "overview response", string(r.Body))
	items := h.overview(nil)
	if got := sum.auth.Load(); got != "Bearer "+customKey {
		t.Fatalf("site got Authorization %v", got)
	}

	s := items[1]
	cs := s["custom"].(map[string]any)
	cur := s["currency"].(map[string]any)
	if s["state"] != "low" || s["balance"] != 4.5 || s["total_used"] != float64(7) || s["today_spend"] != 0.5 || s["host"] != strings.TrimPrefix(sum.URL, "http://") ||
		cur["code"] != "USD" || cs["balance_basis"] != "sum" || cs["total_quota"] != float64(10) || len(cs["plans"].([]any)) != 2 {
		t.Fatalf("sum account = %v", s)
	}
	if s["last_success_at"] == nil || s["primary_failure"] != nil {
		t.Fatalf("sum account status = %v", s)
	}

	f := items[2]
	fc := f["custom"].(map[string]any)
	if f["state"] != "ok" || f["balance"] != float64(120) || fc["balance_basis"] != "first" || f["currency"].(map[string]any)["code"] != "UNIT" || f["currency"].(map[string]any)["symbol"] != "credits" {
		t.Fatalf("first account = %v", f)
	}

	inv := items[3]
	if inv["state"] != "needs_action" || inv["primary_failure"].(map[string]any)["code"] != "reported_invalid" ||
		inv["custom"].(map[string]any)["invalid_message"] != "synthetic: key disabled" || inv["last_success_at"] != nil {
		t.Fatalf("invalid account = %v", inv)
	}

	// Cached for 60 s like built-in accounts; force refreshes.
	hits := sum.hits.Load()
	if again := h.overview(nil); again[1]["cached"] != true || sum.hits.Load() != hits {
		t.Fatal("custom query was not cached")
	}
	h.overview(url.Values{"force": {"1"}})
	if sum.hits.Load() != hits+1 {
		t.Fatal("force did not rerun the custom query")
	}

	// List keeps secrets masked; logs never carry any placeholder value.
	list := h.do("GET", RoutePrefix+"/accounts", nil, nil)
	assertNoCustomSecrets(t, "list response", string(list.Body))
	logs := h.logs.String()
	if !strings.Contains(logs, "custom query failed") {
		t.Fatalf("expected a failure log, got %q", logs)
	}
	for _, base := range []string{sum.URL, first.URL, invalid.URL} {
		for _, v := range customValues(base, h) {
			if strings.Contains(logs, v) {
				t.Fatalf("plugin log contains the placeholder value %q", v)
			}
		}
	}
	if strings.Contains(logs, "key disabled") {
		t.Fatal("plugin log contains script output")
	}
}

func TestTestScriptReportsResultsAndErrorsWithoutSaving(t *testing.T) {
	h := newHarness(t)
	site := newCustomSite(t, `{"ok": true, "plans": [{"name": "monthly", "left": 3, "used": 7, "total": 10, "unit": "USD", "today": 0.5}]}`)
	before := h.snapshot()

	st, res := h.testConnection(customInput("", site.URL, nil))
	c, _ := res["custom"].(map[string]any)
	if st != 200 || res["ok"] != true || res["source"] != "custom" || res["state"] != "ok" || c == nil ||
		c["balance"] != float64(3) || c["total_used"] != float64(7) || c["today_spend"] != 0.5 || c["total_quota"] != float64(10) {
		t.Fatalf("test = %d %v", st, res)
	}
	h.assertUnchanged(before)

	// A syntax error on the line that holds the apiKey, and a runtime error
	// whose message quotes both secrets: masked in the route response.
	for _, script := range []string{
		"({\n  request: { url: \"{{baseUrl}}\", headers: { Authorization: \"Bearer {{apiKey}}\" oops } },\n  extractor: function (r) { return r; }\n})",
		`throw new Error("{{apiKey}} / {{accessToken}}")`,
	} {
		in := customInput("", site.URL, nil)
		in["custom_script"] = script
		r := h.do("POST", RoutePrefix+"/accounts/test", nil, in)
		assertNoCustomSecrets(t, "test response", string(r.Body))
		res := r.json(t)
		c := res["custom"].(map[string]any)
		if r.Status != 200 || res["ok"] != false || res["failure"].(map[string]any)["code"] != "script_error" || c["script_error"] == "" {
			t.Fatalf("script error test = %d %v", r.Status, res)
		}
	}

	// On edit, blank and masked secrets mean the stored ones.
	acc := h.createCustom("Saved", site.URL, nil)
	before = h.snapshot()
	in := customInput("", site.URL, nil)
	in["id"] = acc["id"]
	in["custom_placeholders"] = map[string]any{"baseUrl": site.URL, "apiKey": "", "accessToken": store.Mask(customToken), "userId": customUserID}
	if st, res := h.testConnection(in); st != 200 || res["ok"] != true {
		t.Fatalf("edit test = %d %v", st, res)
	}
	if site.auth.Load() != "Bearer "+customKey {
		t.Fatalf("stored apiKey not used: %v", site.auth.Load())
	}
	h.assertUnchanged(before)
	for _, v := range customValues(site.URL, h) {
		if strings.Contains(h.logs.String(), v) {
			t.Fatalf("plugin log contains the placeholder value %q", v)
		}
	}
}

func TestTestScriptTimeout(t *testing.T) {
	h := newHarness(t)
	h.p.custom.Timeout = 300 * time.Millisecond
	in := customInput("", "https://api.example.com", nil)
	in["custom_script"] = `while (true) {}`
	st, res := h.testConnection(in)
	f := res["failure"].(map[string]any)
	if st != 200 || res["ok"] != false || f["code"] != "query_timeout" || f["diagnostic"] != "SCRIPT_TIMEOUT" || res["state"] != "needs_action" {
		t.Fatalf("timeout test = %d %v", st, res)
	}
}

// The management calls return by the limit even when the script is stuck in
// work the engine cannot interrupt: here regexp backtracking that takes
// several times the limit (and far longer under -race).
func TestCustomQueryCallsReturnByTheLimit(t *testing.T) {
	h := newHarness(t)
	h.p.custom.Timeout = 100 * time.Millisecond
	site := newCustomSite(t, `{"s": "`+strings.Repeat("a", 23)+`!"}`)
	in := customInput("Regex", site.URL, nil)
	in["custom_script"] = `({ request: { url: "{{baseUrl}}" }, extractor: function (r) { /^(?=a)(a+)+$/.test(r.s); return { remaining: 1, unit: "USD" }; } })`
	r := h.do("POST", RoutePrefix+"/accounts", nil, in)
	if r.Status != http.StatusCreated {
		t.Fatalf("create: %d %s", r.Status, r.Body)
	}
	in["id"] = r.json(t)["account"].(map[string]any)["id"]

	begin := time.Now()
	st, res := h.testConnection(in)
	if took := time.Since(begin); took > h.p.custom.Timeout+time.Second {
		t.Fatalf("test-script returned after %v", took)
	}
	if f, _ := res["failure"].(map[string]any); st != 200 || f["code"] != "query_timeout" || f["diagnostic"] != "SCRIPT_TIMEOUT" {
		t.Fatalf("test-script = %d %v", st, res)
	}
	// The backtracking still runs: a forced refresh waits for it within its
	// own limit, then reports it instead of starting a second one.
	begin = time.Now()
	items := h.overview(url.Values{"force": {"1"}})
	if took := time.Since(begin); took > h.p.custom.Timeout+time.Second {
		t.Fatalf("overview returned after %v", took)
	}
	if f, _ := items[0]["primary_failure"].(map[string]any); f["code"] != "query_timeout" || f["diagnostic"] != "PREVIOUS_QUERY_RUNNING" {
		t.Fatalf("overview = %v", items[0])
	}
}

// stuckDoer never answers until released, ignoring cancellation.
type stuckDoer struct {
	n       atomic.Int32
	release chan struct{}
}

func (d *stuckDoer) Do(context.Context, outbound.Request) (*outbound.Response, error) {
	d.n.Add(1)
	<-d.release
	return &outbound.Response{StatusCode: http.StatusOK, Body: []byte(`{"ok": true, "plans": []}`)}, nil
}

// Forced refreshes and test-script runs of an account whose query is still
// stuck past its limit do not start more work.
func TestStuckCustomQueryDoesNotPileUp(t *testing.T) {
	h := newHarness(t)
	d := &stuckDoer{release: make(chan struct{})}
	t.Cleanup(func() { close(d.release) })
	h.p.custom.HTTP, h.p.custom.Timeout = d, 200*time.Millisecond
	acc := h.createCustom("Stuck", "https://api.example.com", nil)

	diag := func(item map[string]any) any {
		f, _ := item["primary_failure"].(map[string]any)
		return f["diagnostic"]
	}
	if got := diag(h.overview(url.Values{"force": {"1"}})[0]); got != "REQUEST_TIMEOUT" {
		t.Fatalf("first refresh: %v", got)
	}
	for range 3 {
		if got := diag(h.overview(url.Values{"force": {"1"}})[0]); got != "PREVIOUS_QUERY_RUNNING" {
			t.Fatalf("forced refresh: %v", got)
		}
	}
	in := customInput("", "https://api.example.com", nil)
	in["id"] = acc["id"]
	if _, res := h.testConnection(in); res["failure"].(map[string]any)["diagnostic"] != "PREVIOUS_QUERY_RUNNING" {
		t.Fatalf("test-script of the account: %v", res)
	}
	if n := d.n.Load(); n != 1 {
		t.Fatalf("%d requests started, want 1", n)
	}
}

func TestUpdatingCustomAccountDropsItsCache(t *testing.T) {
	h := newHarness(t)
	site := newCustomSite(t, `{"ok": true, "plans": [{"name": "a", "left": 1, "unit": "USD"}]}`)
	acc := h.createCustom("A", site.URL, nil)
	h.overview(nil)
	in := customInput("A", site.URL, nil)
	in["id"] = acc["id"]
	in["custom_placeholders"] = map[string]any{"baseUrl": site.URL, "userId": customUserID}
	if r := h.do("PUT", RoutePrefix+"/accounts", nil, in); r.Status != 200 {
		t.Fatalf("update: %d %s", r.Status, r.Body)
	}
	raw, _ := os.ReadFile(filepath.Join(h.dir, store.FileName))
	if !strings.Contains(string(raw), customKey) || !strings.Contains(string(raw), customToken) {
		t.Fatal("blank secrets were not kept on update")
	}
	if items := h.overview(nil); items[0]["cached"] != false {
		t.Fatal("updated account was served from the cache")
	}
}

// Failures the plugin explains itself reach the page as a reason code (no
// English sentence), so the page can word them in zh or en.
func TestTestScriptReasonsAreCodes(t *testing.T) {
	h := newHarness(t)
	site := newCustomSite(t, `{"ok": true}`)
	for script, want := range map[string]map[string]any{
		`({ request: { url: "ftp://example.com/x" }, extractor: function (r) { return r; } })`:                           {"code": "url_scheme"},
		`({ request: { url: "{{baseUrl}}/x", headers: { "Bad Header": "v" } }, extractor: function (r) { return r; } })`: {"code": "header_name_invalid"},
		`({ request: { url: "{{baseUrl}}/x" }, extractor: function (r) { return { remaining: "ten", unit: "USD" }; } })`: {"code": "field_not_number", "field": "result.remaining"},
	} {
		in := customInput("", site.URL, nil)
		in["custom_script"] = script
		_, res := h.testConnection(in)
		c := res["custom"].(map[string]any)
		reason, _ := c["script_reason"].(map[string]any)
		if res["failure"].(map[string]any)["code"] != "script_error" || c["script_error"] != nil || len(reason) != len(want) {
			t.Fatalf("%s: %v", script, res)
		}
		for k, v := range want {
			if reason[k] != v {
				t.Fatalf("%s: script_reason %v, want %v", script, reason, want)
			}
		}
	}
}

// Today's spend carries the unit of the plan that provided it: here a CNY
// plan behind a USD first plan (balance basis "first"). Built-in accounts
// report their display currency.
func TestTodaySpendCarriesItsOwnCurrency(t *testing.T) {
	h := newHarness(t)
	site := newCustomSite(t, `{"ok": true, "plans": [
		{"name": "dollars", "left": 10, "unit": "USD"},
		{"name": "yuan", "left": 5, "unit": "CNY", "today": 7}]}`)
	u := newUpstream(t)
	h.create(u, "Built-in", secretPAT, nil)
	h.createCustom("Mixed", site.URL, nil)
	h.createCustom("No today", newCustomSite(t, `{"ok": true, "plans": [{"name": "a", "left": 1, "unit": "USD"}]}`).URL, nil)

	code := func(v any) any {
		m, _ := v.(map[string]any)
		return m["code"]
	}
	items := h.overview(nil)
	if b := items[0]; b["today_spend"] == nil || code(b["today_spend_currency"]) != code(b["currency"]) {
		t.Fatalf("built-in = %v", b)
	}
	m := items[1]
	if code(m["currency"]) != "USD" || m["balance"] != float64(10) || m["today_spend"] != float64(7) || code(m["today_spend_currency"]) != "CNY" {
		t.Fatalf("mixed = %v", m)
	}
	if n := items[2]; n["today_spend"] != nil || n["today_spend_currency"] != nil {
		t.Fatalf("no today = %v", n)
	}

	_, res := h.testConnection(customInput("", site.URL, nil))
	c := res["custom"].(map[string]any)
	if code(res["currency"]) != "USD" || c["today_spend"] != float64(7) || code(c["today_spend_currency"]) != "CNY" {
		t.Fatalf("test-script = %v", res)
	}
}
