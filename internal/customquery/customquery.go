// Package customquery is the custom-query data source: a user script in the
// cc-switch usage-script format evaluates to { request, extractor }; the
// plugin sends the request through the outbound entry and hands the JSON
// response to extractor, which returns one item or an array of items.
//
// Scripts run in an embedded pure-Go JavaScript engine (goja) with no
// network, file or module access: the runtime only has the ECMAScript
// built-ins. Each query, request plus script, has one time limit: the caller
// gets its answer when it passes, running script code is interrupted, and
// work the engine cannot interrupt (compiling, native built-ins) is left to
// finish in the background, one such run per account at most. Memory cannot
// be limited.
package customquery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/dop251/goja"
	"github.com/dop251/goja/parser"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/newapi"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/outbound"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/status"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/store"
)

const (
	// Timeout bounds one whole query: script evaluation, the request and extractor.
	Timeout = 10 * time.Second
	// MaxResponseBytes caps the response handed to extractor.
	MaxResponseBytes int64 = 1 << 20
	// ProbeName is the endpoint name of the single probe a custom query counts as.
	ProbeName = "custom_query"
	// UnitCode is the currency code for a script unit other than USD or CNY;
	// the unit string itself is the currency symbol and is shown verbatim.
	UnitCode = "UNIT"

	scriptName  = "script"
	maxText     = 500
	maxCallSize = 4096
)

// Diagnostics of script failures (status.Failure.Diagnostic).
const (
	DiagSyntax         = "SCRIPT_SYNTAX"
	DiagRuntime        = "SCRIPT_RUNTIME"
	DiagRequest        = "SCRIPT_REQUEST"
	DiagRequestURL     = "REQUEST_URL"
	DiagRequestScheme  = "REQUEST_SCHEME"
	DiagRequestMethod  = "REQUEST_METHOD"
	DiagRequestHeader  = "REQUEST_HEADER"
	DiagResult         = "SCRIPT_RESULT"
	DiagScriptTimeout  = "SCRIPT_TIMEOUT"
	DiagRequestTimeout = "REQUEST_TIMEOUT"
	// DiagBusy: the account's previous query passed its limit inside work
	// the engine cannot interrupt and is still running.
	DiagBusy    = "PREVIOUS_QUERY_RUNNING"
	DiagInvalid = "IS_VALID_FALSE"
)

// Item is one entry of the extractor result (CustomQueryItem). Absent and
// null fields are nil or empty.
type Item struct {
	IsValid        *bool
	InvalidMessage string
	Remaining      *float64
	Used           *float64
	Total          *float64
	// Unit is the display currency: USD and CNY are recognized, anything
	// else is shown verbatim.
	Unit       string
	PlanName   string
	Extra      string
	TodaySpend *float64
}

// Result is the outcome of one custom query.
type Result struct {
	// Items is the extractor result; empty when Failure is set.
	Items []Item
	// Failure is nil when the script ran and returned a usable result.
	Failure *status.Failure
	// Message is the script or engine error text for the page, with the
	// substituted apiKey and accessToken masked. Never log it.
	Message   string
	FetchedAt time.Time
}

// Runner runs custom queries.
type Runner struct {
	HTTP outbound.Doer
	// Now returns the current time in CPA's local zone; {{todayStart}} is its
	// local midnight. Tests inject a fixed zone.
	Now func() time.Time
	// Timeout bounds each query; zero means Timeout.
	Timeout time.Duration

	mu sync.Mutex
	// busy holds, per account ID, a channel closed when that account's
	// running query ends, including one its caller stopped waiting for.
	// Unsaved accounts (test-script without an ID) share the empty ID.
	busy map[string]chan struct{}
}

// Run queries account a once. It never returns an error: every failure is
// classified into Result.Failure. It returns by the time limit even when the
// script is stuck in work the engine cannot interrupt.
func (rn *Runner) Run(ctx context.Context, a store.Account) Result {
	now := rn.Now()
	limit := rn.Timeout
	if limit <= 0 {
		limit = Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	res := Result{FetchedAt: now}

	release, ok := rn.acquire(ctx, a.ID)
	if !ok {
		res.Failure = &status.Failure{Code: status.QueryTimeout, Diagnostic: DiagBusy}
		return res
	}
	m := newMasker(a.CustomPlaceholders)
	src := substitute(a.CustomScript, a.CustomPlaceholders, newapi.LocalMidnight(now).Unix(), now.Unix())
	var (
		requesting atomic.Bool
		items      []Item
		f          *status.Failure
		msg        string
		done       = make(chan struct{})
	)
	go func() {
		defer release()
		defer close(done)
		items, f, msg = rn.run(ctx, src, outbound.Proxy(a.Proxy), &requesting)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		select {
		case <-done: // finished at the deadline: its own answer is more precise
		default:
			// Left running; it holds the account's slot until it ends.
			diag := DiagScriptTimeout
			if requesting.Load() {
				diag = DiagRequestTimeout
			}
			res.Failure = &status.Failure{Code: status.QueryTimeout, Diagnostic: diag}
			return res
		}
	}
	if f != nil {
		res.Failure, res.Message = f, clip(m.mask(msg))
		return res
	}
	for i := range items {
		it := &items[i]
		it.InvalidMessage = clip(m.mask(it.InvalidMessage))
		it.Unit = clip(m.mask(it.Unit))
		it.PlanName = clip(m.mask(it.PlanName))
		it.Extra = clip(m.mask(it.Extra))
	}
	res.Items = items
	return res
}

// acquire waits until no other query of the account is running, so work
// left running past a limit never piles up behind repeated refreshes. It
// gives up when ctx ends.
func (rn *Runner) acquire(ctx context.Context, id string) (release func(), ok bool) {
	for {
		rn.mu.Lock()
		running, held := rn.busy[id]
		if !held {
			ch := make(chan struct{})
			if rn.busy == nil {
				rn.busy = map[string]chan struct{}{}
			}
			rn.busy[id] = ch
			rn.mu.Unlock()
			return func() {
				rn.mu.Lock()
				delete(rn.busy, id)
				rn.mu.Unlock()
				close(ch)
			}, true
		}
		rn.mu.Unlock()
		select {
		case <-running:
		case <-ctx.Done():
			return nil, false
		}
	}
}

// errDeadline is the value the engine is interrupted with.
var errDeadline = errors.New("custom query time limit reached")

// prelude defines the plugin's helpers before the user script runs, so they
// keep the original built-ins even if the script replaces them. Every value
// that comes from the script is turned into a string inside the engine:
// converting a script object from Go could run script code outside the
// engine's interrupt and error handling.
const prelude = `(function () {
  var stringify = JSON.stringify, parse = JSON.parse, str = String, own = Object.prototype.hasOwnProperty;
  function describe(e) {
    try {
      if (e !== null && typeof e === "object" && typeof e.name === "string" && typeof e.message === "string") {
        return e.name + ": " + e.message;
      }
      return "Uncaught " + str(e);
    } catch (_) {
      return "Uncaught exception";
    }
  }
  function request(c) {
    if (c === null || (typeof c !== "object" && typeof c !== "function")) {
      throw new TypeError("the script must evaluate to an object { request, extractor }");
    }
    var r = c.request;
    if (r === null || typeof r !== "object") throw new TypeError("request is missing or is not an object");
    if (typeof c.extractor !== "function") throw new TypeError("extractor is missing or is not a function");
    var h = r.headers, headers = {};
    if (h !== undefined && h !== null) {
      if (typeof h !== "object") throw new TypeError("request.headers must be an object");
      for (var k in h) {
        if (own.call(h, k) && h[k] !== undefined && h[k] !== null) headers[k] = str(h[k]);
      }
    }
    var body = r.body;
    if (body !== undefined && body !== null && typeof body !== "string") throw new TypeError("request.body must be a string");
    return stringify({
      url: typeof r.url === "string" ? r.url : "",
      method: r.method === undefined || r.method === null ? "GET" : str(r.method),
      headers: headers,
      body: typeof body === "string" ? body : null
    });
  }
  function extract(c, body) {
    var out = stringify(c.extractor.call(c, parse(body)));
    return out === undefined ? "" : out;
  }
  return { describe: describe, request: request, extract: extract };
})()`

type helpers struct {
	describe, request, extract goja.Callable
}

// run evaluates the script, sends its request and runs extractor. On
// failure it returns the failure and the unmasked message for the page.
// requesting is set while the request is in flight.
func (rn *Runner) run(ctx context.Context, src string, proxy outbound.Proxy, requesting *atomic.Bool) ([]Item, *status.Failure, string) {
	prg, err := compile(src)
	if err != nil {
		return nil, scriptFailure(DiagSyntax), err.Error()
	}
	vm := goja.New()
	vm.SetMaxCallStackSize(maxCallSize)
	// eval and new Function compile at run time with these options.
	vm.SetParserOptions(parser.WithDisableSourceMaps)
	stop := context.AfterFunc(ctx, func() { vm.Interrupt(errDeadline) })
	defer stop()

	hv, err := vm.RunString(prelude)
	if err != nil {
		return nil, engineFailure(err), engineMessage(err)
	}
	var h helpers
	ho := hv.ToObject(vm)
	h.describe, _ = goja.AssertFunction(ho.Get("describe"))
	h.request, _ = goja.AssertFunction(ho.Get("request"))
	h.extract, _ = goja.AssertFunction(ho.Get("extract"))

	cfg, err := vm.RunProgram(prg)
	if err != nil {
		return h.fail(err, DiagRuntime)
	}
	rv, err := h.request(goja.Undefined(), cfg)
	if err != nil {
		return h.fail(err, DiagRequest)
	}
	rs, _ := rv.Export().(string)
	req, f, msg := buildRequest(rs)
	if f != nil {
		return nil, f, msg
	}
	req.Proxy, req.MaxResponseBytes = proxy, MaxResponseBytes

	requesting.Store(true)
	resp, err := rn.HTTP.Do(ctx, req)
	requesting.Store(false)
	if f := classify(ctx, resp, err); f != nil {
		return nil, f, ""
	}
	out, err := h.extract(goja.Undefined(), cfg, vm.ToValue(string(resp.Body)))
	if err != nil {
		return h.fail(err, DiagRuntime)
	}
	rs, _ = out.Export().(string)
	items, msg := decodeResult(rs)
	if msg != "" {
		return nil, scriptFailure(DiagResult), msg
	}
	return items, nil, ""
}

// compile parses the script without source maps: the parser's default
// loader would read the file a //# sourceMappingURL comment names, and a
// script must never cause file access.
func compile(src string) (*goja.Program, error) {
	ast, err := goja.Parse(scriptName, src, parser.WithDisableSourceMaps)
	if err != nil {
		return nil, err
	}
	return goja.CompileAST(ast, false)
}

func scriptFailure(diag string) *status.Failure {
	return &status.Failure{Code: status.ScriptError, Diagnostic: diag}
}

// engineFailure classifies an error the engine raised outside script code.
func engineFailure(err error) *status.Failure {
	var ie *goja.InterruptedError
	if errors.As(err, &ie) {
		return &status.Failure{Code: status.QueryTimeout, Diagnostic: DiagScriptTimeout}
	}
	return scriptFailure(DiagRuntime)
}

// engineMessage is the page text for an error the engine raised itself. A
// timeout has none: its failure class already says everything.
func engineMessage(err error) string {
	var ie *goja.InterruptedError
	var so *goja.StackOverflowError
	switch {
	case errors.As(err, &ie):
		return ""
	case errors.As(err, &so):
		return "RangeError: Maximum call stack size exceeded"
	}
	return "Script failed"
}

// fail turns an error from script code into a failure and a message.
// A thrown value is described by the engine itself (see prelude).
func (h helpers) fail(err error, diag string) ([]Item, *status.Failure, string) {
	var ie *goja.InterruptedError
	var so *goja.StackOverflowError
	var ex *goja.Exception
	switch {
	case errors.As(err, &ie), errors.As(err, &so):
		return nil, engineFailure(err), engineMessage(err)
	case errors.As(err, &ex):
		msg := "Uncaught exception"
		if v, derr := h.describe(goja.Undefined(), ex.Value()); derr == nil {
			msg, _ = v.Export().(string)
		} else if errors.As(derr, &ie) {
			return nil, engineFailure(derr), engineMessage(derr)
		}
		if pos := position(ex); pos != "" {
			msg += " at " + pos
		}
		return nil, scriptFailure(diag), msg
	}
	return nil, scriptFailure(diag), "Script failed"
}

// position is where in the user script the exception was raised.
func position(ex *goja.Exception) string {
	for _, fr := range ex.Stack() {
		if fr.SrcName() == scriptName {
			p := fr.Position()
			return scriptName + ":" + strconv.Itoa(p.Line) + ":" + strconv.Itoa(p.Column)
		}
	}
	return ""
}

// buildRequest checks the request the script described. Messages never
// repeat the URL or header values: they may hold the substituted secrets.
func buildRequest(raw string) (outbound.Request, *status.Failure, string) {
	var r struct {
		URL     string            `json:"url"`
		Method  string            `json:"method"`
		Headers map[string]string `json:"headers"`
		Body    *string           `json:"body"`
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return outbound.Request{}, scriptFailure(DiagRequest), "request could not be read"
	}
	u, err := url.Parse(strings.TrimSpace(r.URL))
	if err != nil || u.Host == "" || u.Opaque != "" {
		if err == nil && u.Scheme != "" && !isHTTP(u.Scheme) {
			return outbound.Request{}, scriptFailure(DiagRequestScheme), "request.url must use http or https"
		}
		return outbound.Request{}, scriptFailure(DiagRequestURL), "request.url must be an absolute http or https URL"
	}
	if !isHTTP(u.Scheme) {
		return outbound.Request{}, scriptFailure(DiagRequestScheme), "request.url must use http or https"
	}
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if !isToken(method) {
		return outbound.Request{}, scriptFailure(DiagRequestMethod), "request.method is not a valid HTTP method"
	}
	h := http.Header{}
	for k, v := range r.Headers {
		if !isToken(k) {
			return outbound.Request{}, scriptFailure(DiagRequestHeader), "request.headers has an invalid header name"
		}
		if strings.ContainsAny(v, "\r\n\x00") {
			return outbound.Request{}, scriptFailure(DiagRequestHeader), "request.headers has an invalid header value"
		}
		h.Set(k, v)
	}
	req := outbound.Request{Method: method, URL: u.String(), Header: h}
	if r.Body != nil {
		req.Body = []byte(*r.Body)
	}
	return req, nil, ""
}

func isHTTP(scheme string) bool {
	s := strings.ToLower(scheme)
	return s == "http" || s == "https"
}

// isToken reports an RFC 9110 token (method and header names).
func isToken(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// classify maps the upstream outcome: transport failures and non-2xx answers
// use the shared classes; a 2xx answer that is not JSON is a bad response.
func classify(ctx context.Context, resp *outbound.Response, err error) *status.Failure {
	if err != nil || resp == nil {
		var oe *outbound.Error
		if errors.As(err, &oe) && (oe.Kind == outbound.ErrTimeout || oe.Kind == outbound.ErrCanceled) && ctx.Err() != nil {
			return &status.Failure{Code: status.QueryTimeout, Diagnostic: DiagRequestTimeout}
		}
		return status.ClassifyHTTP(resp, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return status.ClassifyHTTP(resp, nil)
	}
	if !json.Valid(resp.Body) {
		return &status.Failure{Code: status.BadResponse, HTTPStatus: resp.StatusCode, Diagnostic: "INVALID_JSON"}
	}
	return nil
}

// decodeResult reads the extractor result: one object or a non-empty array
// of objects whose fields, when present and not null, have the right types.
func decodeResult(raw string) ([]Item, string) {
	if raw == "" {
		return nil, "extractor must return an object or an array of objects"
	}
	var list []json.RawMessage
	single := !strings.HasPrefix(raw, "[")
	if single {
		list = []json.RawMessage{json.RawMessage(raw)}
	} else if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, "extractor result could not be read"
	}
	if len(list) == 0 {
		return nil, "extractor returned an empty array"
	}
	items := make([]Item, 0, len(list))
	for i, el := range list {
		where := "result"
		if !single {
			where = "result[" + strconv.Itoa(i) + "]"
		}
		it, msg := decodeItem(el, where)
		if msg != "" {
			return nil, msg
		}
		items = append(items, it)
	}
	return items, ""
}

func decodeItem(raw json.RawMessage, where string) (Item, string) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return Item{}, where + " must be an object"
	}
	var it Item
	var msg string
	present := func(k string) (json.RawMessage, bool) {
		v, ok := fields[k]
		return v, ok && string(v) != "null"
	}
	num := func(k string) *float64 {
		v, ok := present(k)
		if !ok || msg != "" {
			return nil
		}
		var f float64
		if json.Unmarshal(v, &f) != nil {
			msg = where + "." + k + " must be a number or null"
			return nil
		}
		return &f
	}
	text := func(k string) string {
		v, ok := present(k)
		if !ok || msg != "" {
			return ""
		}
		var s string
		if json.Unmarshal(v, &s) != nil {
			msg = where + "." + k + " must be a string or null"
		}
		return s
	}
	if v, ok := present("isValid"); ok {
		var b bool
		if json.Unmarshal(v, &b) != nil {
			return Item{}, where + ".isValid must be a boolean or null"
		}
		it.IsValid = &b
	}
	it.InvalidMessage = text("invalidMessage")
	it.Remaining, it.Used, it.Total = num("remaining"), num("used"), num("total")
	it.Unit, it.PlanName, it.Extra = text("unit"), text("planName"), text("extra")
	it.TodaySpend = num("todaySpend")
	return it, msg
}

// substitute replaces the placeholders in one pass, so a value can never
// introduce another placeholder. String values are escaped for any JS
// string literal (quotes, template literals); {{todayStart}} and {{now}}
// are Unix seconds. Unknown {{…}} are left alone.
func substitute(src string, ph store.Placeholders, todayStart, now int64) string {
	vals := [...][2]string{
		{"{{baseUrl}}", escapeJS(ph.BaseURL)},
		{"{{apiKey}}", escapeJS(ph.APIKey)},
		{"{{accessToken}}", escapeJS(ph.AccessToken)},
		{"{{userId}}", escapeJS(ph.UserID)},
		{"{{todayStart}}", strconv.FormatInt(todayStart, 10)},
		{"{{now}}", strconv.FormatInt(now, 10)},
	}
	var b strings.Builder
	b.Grow(len(src))
next:
	for {
		i := strings.Index(src, "{{")
		if i < 0 {
			b.WriteString(src)
			return b.String()
		}
		b.WriteString(src[:i])
		src = src[i:]
		for _, kv := range vals {
			if strings.HasPrefix(src, kv[0]) {
				b.WriteString(kv[1])
				src = src[len(kv[0]):]
				continue next
			}
		}
		b.WriteString("{{")
		src = src[2:]
	}
}

func escapeJS(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\'':
			b.WriteString(`\'`)
		case '`':
			b.WriteString("\\`")
		case '$':
			b.WriteString(`\$`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case 0:
			b.WriteString(`\u0000`)
		case '\u2028':
			b.WriteString(`\u2028`)
		case '\u2029':
			b.WriteString(`\u2029`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// masker hides the substituted apiKey and accessToken in text that goes to
// the page, in both their raw and their escaped (as substituted) forms.
type masker struct{ r *strings.Replacer }

func newMasker(ph store.Placeholders) masker {
	var pairs []string
	for _, s := range []string{ph.APIKey, ph.AccessToken} {
		if s == "" {
			continue
		}
		pairs = append(pairs, s, store.Mask(s))
		if e := escapeJS(s); e != s {
			pairs = append(pairs, e, store.Mask(s))
		}
	}
	if len(pairs) == 0 {
		return masker{}
	}
	return masker{strings.NewReplacer(pairs...)}
}

func (m masker) mask(s string) string {
	if m.r == nil || s == "" {
		return s
	}
	return m.r.Replace(s)
}

// clip bounds a script-supplied string; clipping happens after masking so a
// cut can never expose part of a secret that masking would have hidden.
func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxText {
		return s
	}
	r := []rune(s)
	return string(r[:maxText]) + "…"
}

// Basis says how the balance was formed from several items.
type Basis string

const (
	// BasisSum: every item has the same unit; the balance is the sum of remaining.
	BasisSum Basis = "sum"
	// BasisFirst: units differ; the balance is the first item's remaining.
	BasisFirst Basis = "first"
)

// Summary is a Result judged against the account's threshold.
type Summary struct {
	State status.State
	// Failure is the query failure, or ReportedInvalid when an item has
	// isValid: false; nil otherwise.
	Failure  *status.Failure
	Probes   []status.Probe
	Currency *newapi.Currency
	// Balance, TotalUsed and TotalQuota follow Basis; TodaySpend comes from
	// the first item that provides it.
	Balance, TotalUsed, TotalQuota, TodaySpend *float64
	// Basis is empty for a single item.
	Basis Basis
	// InvalidMessage is the invalidMessage of the item that reported isValid: false.
	InvalidMessage string
}

// Summarize applies the result-mapping and account-status rules.
func Summarize(r Result, threshold *float64) Summary {
	var s Summary
	f := r.Failure
	if f == nil {
		if i := invalidItem(r.Items); i >= 0 {
			f = &status.Failure{Code: status.ReportedInvalid, Diagnostic: DiagInvalid}
			s.InvalidMessage = r.Items[i].InvalidMessage
		}
	}
	s.Failure = f
	s.Probes = []status.Probe{{Name: ProbeName, Credentialed: true, Failure: f}}
	if len(r.Items) > 0 {
		pick := r.Items[:1]
		if len(r.Items) > 1 {
			s.Basis = BasisFirst
			if sameUnit(r.Items) {
				s.Basis, pick = BasisSum, r.Items
			}
		}
		c := Currency(pick[0].Unit)
		s.Currency = &c
		s.Balance = sum(pick, func(it Item) *float64 { return it.Remaining })
		s.TotalUsed = sum(pick, func(it Item) *float64 { return it.Used })
		s.TotalQuota = sum(pick, func(it Item) *float64 { return it.Total })
		for _, it := range r.Items {
			if it.TodaySpend != nil {
				v := *it.TodaySpend
				s.TodaySpend = &v
				break
			}
		}
	}
	s.State = status.Decide(s.Probes, s.Balance, threshold)
	return s
}

// Currency maps a script unit to the display currency.
func Currency(unit string) newapi.Currency {
	switch unit {
	case "USD":
		return newapi.Currency{Code: "USD", Symbol: "$"}
	case "CNY":
		return newapi.Currency{Code: "CNY", Symbol: "¥"}
	}
	return newapi.Currency{Code: UnitCode, Symbol: unit}
}

func invalidItem(items []Item) int {
	for i, it := range items {
		if it.IsValid != nil && !*it.IsValid {
			return i
		}
	}
	return -1
}

func sameUnit(items []Item) bool {
	for _, it := range items[1:] {
		if it.Unit != items[0].Unit {
			return false
		}
	}
	return true
}

// sum adds the field over items that provide it; nil when none does.
func sum(items []Item, field func(Item) *float64) *float64 {
	var total float64
	var seen bool
	for _, it := range items {
		if v := field(it); v != nil {
			total += *v
			seen = true
		}
	}
	if !seen {
		return nil
	}
	return &total
}
