package customquery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/outbound"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/status"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/store"
)

const (
	apiKey      = "sk-synthetic-custom-api-key-5001"
	accessToken = "synthetic-custom-access-token-5002"
	userID      = "5003"
)

// zone is CPA's injected local zone; "now" is 09:30 there, so today
// started at 01:30 the previous day in UTC.
var (
	zone = time.FixedZone("UTC+8", 8*3600)
	now  = time.Date(2026, 5, 2, 9, 30, 0, 0, zone)
)

// countingDoer is the real outbound entry that also counts requests.
type countingDoer struct {
	outbound.Doer
	n    atomic.Int32
	last atomic.Pointer[outbound.Request]
}

func (d *countingDoer) Do(ctx context.Context, req outbound.Request) (*outbound.Response, error) {
	d.n.Add(1)
	d.last.Store(&req)
	return d.Doer.Do(ctx, req)
}

func newRunner() (*Runner, *countingDoer) {
	d := &countingDoer{Doer: outbound.New()}
	return &Runner{HTTP: d, Now: func() time.Time { return now }}, d
}

func account(script, base string) store.Account {
	return store.Account{
		Source: store.SourceCustom, Proxy: store.ProxyDirect, CustomScript: script,
		CustomPlaceholders: store.Placeholders{BaseURL: base, APIKey: apiKey, AccessToken: accessToken, UserID: userID},
	}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/cc-switch/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func serveJSON(t *testing.T, check func(r *http.Request), body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func wantFailure(t *testing.T, r Result, code status.Code, diag string) {
	t.Helper()
	if r.Failure == nil || r.Failure.Code != code || (diag != "" && r.Failure.Diagnostic != diag) {
		t.Fatalf("failure = %+v (message %q), want %s/%s", r.Failure, r.Message, code, diag)
	}
}

func TestCCSwitchGeneralTemplateRunsVerbatim(t *testing.T) {
	var auth, ua atomic.Value
	srv := serveJSON(t, func(r *http.Request) {
		if r.URL.Path != "/user/balance" || r.Method != http.MethodGet {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		auth.Store(r.Header.Get("Authorization"))
		ua.Store(r.Header.Get("User-Agent"))
	}, `{"balance": 12.5, "is_active": true}`)
	rn, _ := newRunner()
	r := rn.Run(context.Background(), account(fixture(t, "general.js"), srv.URL))
	if r.Failure != nil {
		t.Fatalf("failure %+v: %s", r.Failure, r.Message)
	}
	if auth.Load() != "Bearer "+apiKey || ua.Load() != "cc-switch/1.0" {
		t.Fatalf("headers: %v / %v", auth.Load(), ua.Load())
	}
	s := Summarize(r, nil)
	if s.State != status.OK || *s.Balance != 12.5 || s.Currency.Code != "USD" || s.Currency.Symbol != "$" || s.Basis != "" {
		t.Fatalf("summary = %+v", s)
	}
}

func TestCCSwitchNewAPITemplateRunsVerbatim(t *testing.T) {
	answer := atomic.Value{}
	answer.Store(`{"success": true, "data": {"quota": 5000000, "used_quota": 1000000, "group": "vip"}}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/self" || r.Header.Get("Authorization") != "Bearer "+accessToken || r.Header.Get("New-Api-User") != userID {
			t.Errorf("request %s auth=%q user=%q", r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("New-Api-User"))
		}
		_, _ = w.Write([]byte(answer.Load().(string)))
	}))
	t.Cleanup(srv.Close)
	rn, _ := newRunner()
	a := account(fixture(t, "newapi.js"), srv.URL)

	s := Summarize(rn.Run(context.Background(), a), nil)
	if s.State != status.OK || *s.Balance != 10 || *s.TotalUsed != 2 || *s.TotalQuota != 12 || s.Currency.Code != "USD" {
		t.Fatalf("summary = %+v", s)
	}
	r := rn.Run(context.Background(), a)
	if r.Items[0].PlanName != "vip" {
		t.Fatalf("plan = %+v", r.Items[0])
	}

	// The template's own failure branch: isValid false with the site message.
	answer.Store(`{"success": false, "message": "synthetic: access token expired"}`)
	r = rn.Run(context.Background(), a)
	s = Summarize(r, nil)
	if s.State != status.NeedsAction || s.Failure.Code != status.ReportedInvalid || s.InvalidMessage != "synthetic: access token expired" {
		t.Fatalf("invalid summary = %+v", s)
	}
}

func TestSingleObjectAndArrayResults(t *testing.T) {
	srv := serveJSON(t, nil, `{}`)
	rn, _ := newRunner()
	run := func(result string) Summary {
		t.Helper()
		script := `({ request: { url: "{{baseUrl}}/u" }, extractor: function () { return ` + result + `; } })`
		r := rn.Run(context.Background(), account(script, srv.URL))
		if r.Failure != nil {
			t.Fatalf("%s: %+v %s", result, r.Failure, r.Message)
		}
		return Summarize(r, ptr(5))
	}

	one := run(`{ remaining: 3, used: 1, total: 4, unit: "CNY" }`)
	if one.Basis != "" || *one.Balance != 3 || one.Currency.Code != "CNY" || one.Currency.Symbol != "¥" || one.State != status.Low {
		t.Fatalf("single = %+v", one)
	}

	// Same unit everywhere: the balance is the sum of remaining (合计).
	sum := run(`[{ planName: "a", remaining: 4, used: 1, total: 5, unit: "credits" }, { planName: "b", remaining: 6.5, total: 10, unit: "credits" }]`)
	if sum.Basis != BasisSum || *sum.Balance != 10.5 || *sum.TotalUsed != 1 || *sum.TotalQuota != 15 || sum.State != status.OK {
		t.Fatalf("sum = %+v", sum)
	}
	if sum.Currency.Code != UnitCode || sum.Currency.Symbol != "credits" {
		t.Fatalf("other units are shown verbatim: %+v", sum.Currency)
	}

	// Units differ: the balance is the first item's (第一项).
	first := run(`[{ remaining: 2, used: 8, total: 10, unit: "USD" }, { remaining: 300, unit: "CNY" }]`)
	if first.Basis != BasisFirst || *first.Balance != 2 || *first.TotalUsed != 8 || *first.TotalQuota != 10 || first.Currency.Code != "USD" || first.State != status.Low {
		t.Fatalf("first = %+v", first)
	}
}

func TestTodaySpendAndTimePlaceholdersUseCPALocalZone(t *testing.T) {
	start := time.Date(2026, 5, 2, 0, 0, 0, 0, zone).Unix()
	var gotStart, gotNow atomic.Value
	srv := serveJSON(t, func(r *http.Request) {
		gotStart.Store(r.URL.Query().Get("start"))
		gotNow.Store(r.URL.Query().Get("end"))
	}, `{"spend": 1.25}`)
	rn, _ := newRunner()
	script := `({
  request: { url: "{{baseUrl}}/usage?start={{todayStart}}&end={{now}}", method: "GET" },
  extractor: function (r) {
    return [
      { planName: "no spend", remaining: 1, unit: "USD" },
      { planName: "first spend", remaining: 2, unit: "USD", todaySpend: r.spend },
      { planName: "later spend", todaySpend: 99, unit: "USD", extra: String({{todayStart}}) }
    ];
  }
})`
	r := rn.Run(context.Background(), account(script, srv.URL))
	if r.Failure != nil {
		t.Fatalf("%+v %s", r.Failure, r.Message)
	}
	if gotStart.Load() != strconv.FormatInt(start, 10) || gotNow.Load() != strconv.FormatInt(now.Unix(), 10) {
		t.Fatalf("todayStart=%v now=%v, want %d/%d", gotStart.Load(), gotNow.Load(), start, now.Unix())
	}
	if r.Items[2].Extra != strconv.FormatInt(start, 10) {
		t.Fatalf("number placeholder in code = %q", r.Items[2].Extra)
	}
	if s := Summarize(r, nil); *s.TodaySpend != 1.25 || *s.Balance != 3 {
		t.Fatalf("summary = %+v", s)
	}
}

func TestInfiniteLoopIsInterruptedWithinTheQueryLimit(t *testing.T) {
	srv := serveJSON(t, nil, `{}`)
	rn, _ := newRunner()
	begin := time.Now()
	r := rn.Run(context.Background(), account(`({ request: { url: "{{baseUrl}}" }, extractor: function () { while (true) {} } })`, srv.URL))
	took := time.Since(begin)
	wantFailure(t, r, status.QueryTimeout, DiagScriptTimeout)
	if took > Timeout+500*time.Millisecond {
		t.Fatalf("interrupted after %v, limit %v", took, Timeout)
	}

	// Shorter limits for the other places a script can stall.
	rn.Timeout = 300 * time.Millisecond
	wantFailure(t, rn.Run(context.Background(), account(`for (;;) {}`, srv.URL)), status.QueryTimeout, DiagScriptTimeout)
	// A thrown value whose toString never returns is described inside the engine, so it is interrupted too.
	wantFailure(t, rn.Run(context.Background(), account(`throw { toString: function () { for (;;) {} } }`, srv.URL)), status.QueryTimeout, DiagScriptTimeout)

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(slow.Close)
	wantFailure(t, rn.Run(context.Background(), account(`({ request: { url: "{{baseUrl}}" }, extractor: function (r) { return r; } })`, slow.URL)), status.QueryTimeout, DiagRequestTimeout)
}

func TestResponseLargerThanOneMiBIsRejected(t *testing.T) {
	body := func(n int) string { return `{"pad":"` + strings.Repeat("x", n-10) + `"}` }
	script := `({ request: { url: "{{baseUrl}}" }, extractor: function (r) { return { remaining: r.pad.length, unit: "USD" }; } })`
	rn, _ := newRunner()

	exact := serveJSON(t, nil, body(int(MaxResponseBytes)))
	if r := rn.Run(context.Background(), account(script, exact.URL)); r.Failure != nil {
		t.Fatalf("a 1 MiB response was refused: %+v", r.Failure)
	}
	big := serveJSON(t, nil, body(int(MaxResponseBytes)+1))
	wantFailure(t, rn.Run(context.Background(), account(script, big.URL)), status.BadResponse, "RESPONSE_TOO_LARGE")
}

func TestScriptsHaveNoNetworkFileOrModuleAccess(t *testing.T) {
	var trapped atomic.Int32
	trap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { trapped.Add(1) }))
	t.Cleanup(trap.Close)
	srv := serveJSON(t, nil, `{}`)
	rn, d := newRunner()

	// Nothing that reaches the network, files or modules exists in the engine.
	probe := `var names = ["require", "fetch", "XMLHttpRequest", "WebSocket", "process", "module", "exports",
  "Deno", "Bun", "importScripts", "load", "readFile", "open", "console", "setTimeout", "navigator", "os", "fs", "http"];
var present = names.filter(function (n) { return typeof globalThis[n] !== "undefined"; });
({ request: { url: "{{baseUrl}}" }, extractor: function () { return { remaining: present.length, extra: present.join(","), unit: "USD" }; } })`
	r := rn.Run(context.Background(), account(probe, srv.URL))
	if r.Failure != nil || *r.Items[0].Remaining != 0 {
		t.Fatalf("globals present: %+v (%+v %s)", r.Items, r.Failure, r.Message)
	}

	for _, script := range []string{
		`fetch("` + trap.URL + `")`,
		`new XMLHttpRequest().open("GET", "` + trap.URL + `")`,
		`require("http").get("` + trap.URL + `")`,
		`require("fs").readFileSync("/etc/hostname")`,
		`import("` + trap.URL + `/mod.js")`,
		`import fs from "fs";`,
		`({ request: { url: "{{baseUrl}}" }, extractor: function () { return fetch("` + trap.URL + `"); } })`,
	} {
		before := d.n.Load()
		r := rn.Run(context.Background(), account(script, srv.URL))
		if r.Failure == nil || r.Failure.Code != status.ScriptError {
			t.Errorf("%s: failure %+v", script, r.Failure)
		}
		if strings.Contains(script, "extractor") {
			before++ // the declared request is the only one sent
		}
		if d.n.Load() != before {
			t.Errorf("%s: the plugin sent an extra request", script)
		}
	}
	if trapped.Load() != 0 {
		t.Fatalf("a script reached the network %d times", trapped.Load())
	}
}

func TestOnlyHTTPAndHTTPSRequestsAreSent(t *testing.T) {
	rn, d := newRunner()
	for url, diag := range map[string]string{
		"file:///etc/passwd":     DiagRequestScheme,
		"ftp://ftp.example.com/": DiagRequestScheme,
		"javascript:alert(1)":    DiagRequestScheme,
		"data:text/plain,hi":     DiagRequestScheme,
		"/relative/path":         DiagRequestURL,
		"":                       DiagRequestURL,
	} {
		r := rn.Run(context.Background(), account(`({ request: { url: "`+url+`" }, extractor: function (r) { return r; } })`, ""))
		wantFailure(t, r, status.ScriptError, diag)
	}
	if d.n.Load() != 0 {
		t.Fatalf("%d requests were sent", d.n.Load())
	}
	for _, bad := range []string{`method: "GE T"`, `headers: { "Bad Name": "x" }`, `headers: { "X-A": "a\r\nInjected: 1" }`, `body: { a: 1 }`} {
		r := rn.Run(context.Background(), account(`({ request: { url: "https://api.example.com", `+bad+` }, extractor: function (r) { return r; } })`, ""))
		if r.Failure == nil || r.Failure.Code != status.ScriptError {
			t.Errorf("%s: %+v", bad, r.Failure)
		}
	}
	if d.n.Load() != 0 {
		t.Fatalf("%d requests were sent", d.n.Load())
	}
}

func TestPlaceholderValuesCannotBreakOutOfStrings(t *testing.T) {
	srv := serveJSON(t, nil, `{"balance": 1}`)
	rn, d := newRunner()
	a := account(fixture(t, "general.js"), srv.URL)
	a.CustomPlaceholders.APIKey = "k\"; throw 1; //'`${x}\\{{userId}}"
	r := rn.Run(context.Background(), a)
	if r.Failure != nil {
		t.Fatalf("%+v %s", r.Failure, r.Message)
	}
	if got := d.last.Load().Header.Get("Authorization"); got != "Bearer "+a.CustomPlaceholders.APIKey {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestScriptErrorsMaskSubstitutedSecrets(t *testing.T) {
	srv := serveJSON(t, nil, `{"echo": "`+apiKey+`"}`)
	rn, _ := newRunner()
	special := "sk-synthetic\"quoted$key-5004"
	cases := []struct {
		name, script string
		diag         string
	}{
		{"syntax error on the apiKey line", "({\n  request: { url: \"{{baseUrl}}\", headers: { Authorization: \"Bearer {{apiKey}}\" \"{{accessToken}}\" } },\n  extractor: function (r) { return r; }\n})", DiagSyntax},
		{"unterminated string holding the apiKey", "var k = \"{{apiKey}}\n;", DiagSyntax},
		{"bad regexp with the token", "var re = /{{accessToken}}(/;", DiagSyntax},
		{"thrown at top level", `throw new Error("key {{apiKey}} token {{accessToken}}")`, DiagRuntime},
		{"thrown string", `throw "{{apiKey}}"`, DiagRuntime},
		{"thrown in extractor", `({ request: { url: "{{baseUrl}}" }, extractor: function (r) { throw new TypeError(r.echo + " / {{accessToken}}"); } })`, DiagRuntime},
		{"reference error", `{{apiKey}}`, ""},
	}
	for _, c := range cases {
		a := account(c.script, srv.URL)
		r := rn.Run(context.Background(), a)
		if r.Failure == nil || r.Failure.Code != status.ScriptError || (c.diag != "" && r.Failure.Diagnostic != c.diag) {
			t.Errorf("%s: failure %+v (%s)", c.name, r.Failure, r.Message)
			continue
		}
		if r.Message == "" || strings.Contains(r.Message, apiKey) || strings.Contains(r.Message, accessToken) {
			t.Errorf("%s: message %q", c.name, r.Message)
		}
		t.Logf("%s → %s", c.name, r.Message)
	}

	// Strings in the result are masked too, including the escaped form of a value.
	a := account(`({ request: { url: "{{baseUrl}}" }, extractor: function (r) {
  return { planName: r.echo, extra: "{{apiKey}}", invalidMessage: "{{accessToken}}", remaining: 1, unit: "USD" };
} })`, srv.URL)
	r := rn.Run(context.Background(), a)
	b, _ := json.Marshal(r.Items)
	if r.Failure != nil || strings.Contains(string(b), apiKey) || strings.Contains(string(b), accessToken) || !strings.Contains(string(b), store.Mask(apiKey)) {
		t.Fatalf("items = %s (%+v)", b, r.Failure)
	}
	a.CustomPlaceholders.APIKey = special
	a.CustomScript = `throw "x {{apiKey}}"`
	if r := rn.Run(context.Background(), a); strings.Contains(r.Message, special) || strings.Contains(r.Message, escapeJS(special)) {
		t.Fatalf("escaped secret leaked: %q", r.Message)
	}
}

func TestUnusableResultsAndResponses(t *testing.T) {
	rn, _ := newRunner()
	srv := serveJSON(t, nil, `{"v": 1}`)
	for result, want := range map[string]string{
		`undefined`:               "extractor must return",
		`null`:                    "must be an object",
		`[]`:                      "empty array",
		`42`:                      "must be an object",
		`{ remaining: "12" }`:     "result.remaining must be a number",
		`[{}, { isValid: "no" }]`: "result[1].isValid must be a boolean",
		`{ unit: 5 }`:             "result.unit must be a string",
	} {
		r := rn.Run(context.Background(), account(`({ request: { url: "{{baseUrl}}" }, extractor: function () { return `+result+`; } })`, srv.URL))
		wantFailure(t, r, status.ScriptError, DiagResult)
		if !strings.Contains(r.Message, want) {
			t.Errorf("%s: message %q, want %q", result, r.Message, want)
		}
	}
	// Null fields are absent fields.
	r := rn.Run(context.Background(), account(`({ request: { url: "{{baseUrl}}" }, extractor: function () { return { remaining: null, unit: null, isValid: null }; } })`, srv.URL))
	if r.Failure != nil || r.Items[0].Remaining != nil || r.Items[0].IsValid != nil {
		t.Fatalf("nulls: %+v %+v", r.Items, r.Failure)
	}

	for _, c := range []struct {
		status int
		ctype  string
		body   string
		code   status.Code
	}{
		{200, "text/plain", "plain text", status.BadResponse},
		{200, "text/html", "<html>challenge</html>", status.BadResponse},
		{401, "application/json", `{"error":"no"}`, status.CredentialInvalid},
		{502, "text/html", "<html>bad gateway</html>", status.SiteError},
		{404, "application/json", `{}`, status.BadResponse},
	} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", c.ctype)
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}))
		r := rn.Run(context.Background(), account(`({ request: { url: "{{baseUrl}}" }, extractor: function (r) { return { remaining: 1 }; } })`, up.URL))
		up.Close()
		wantFailure(t, r, c.code, "")
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	wantFailure(t, rn.Run(context.Background(), account(`({ request: { url: "{{baseUrl}}" }, extractor: function (r) { return r; } })`, closed.URL)), status.SiteUnreachable, "")
}

func TestSubstituteIsSinglePass(t *testing.T) {
	// Same scan as cc-switch: a substituted value is never scanned again,
	// and an unknown {{…}} (including "{{{baseUrl}}") is left as written.
	ph := store.Placeholders{BaseURL: "https://api.example.com", APIKey: "{{accessToken}}", AccessToken: "secret", UserID: "7"}
	got := substitute("{{apiKey}}|{{userId}}|{{{baseUrl}}}|{{unknown}}|{{now}}|{{todayStart}}|{{baseUrl}}", ph, 100, 200)
	if got != "{{accessToken}}|7|{{{baseUrl}}}|{{unknown}}|200|100|https://api.example.com" {
		t.Fatalf("substitute = %q", got)
	}
}

func ptr(v float64) *float64 { return &v }
