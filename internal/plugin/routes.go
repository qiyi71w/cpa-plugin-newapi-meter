package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/cache"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/customquery"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/newapi"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/outbound"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/status"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/store"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/webui"
)

// managementRequest is the host's management.handle payload.
type managementRequest struct {
	Method         string
	Path           string
	Query          url.Values
	Body           []byte
	HostCallbackID string `json:"host_callback_id"`
}

type managementResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

func (p *Plugin) handle(raw []byte) managementResponse {
	var req managementRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return apiError(http.StatusBadRequest, errorBody{Code: "invalid_request"})
	}
	if req.Path == PagePath {
		if req.Method != http.MethodGet {
			return apiError(http.StatusMethodNotAllowed, errorBody{Code: "method_not_allowed"})
		}
		return p.servePage()
	}
	ctx := outbound.WithCall(context.Background(), outbound.Call{HostCallbackID: req.HostCallbackID})
	switch req.Method + " " + req.Path {
	case "GET " + RoutePrefix + "/accounts":
		return p.listAccounts()
	case "POST " + RoutePrefix + "/accounts":
		return p.createAccount(req.Body)
	case "PUT " + RoutePrefix + "/accounts":
		return p.updateAccount(req.Body)
	case "DELETE " + RoutePrefix + "/accounts":
		return p.deleteAccount(req.Query.Get("id"))
	case "GET " + RoutePrefix + "/overview":
		return p.overview(ctx, req.Query)
	case "POST " + RoutePrefix + "/accounts/test":
		return p.testConnection(ctx, req.Body)
	case "GET " + RoutePrefix + "/detail":
		return p.detail(ctx, req.Query)
	}
	return apiError(http.StatusNotFound, errorBody{Code: "not_found"})
}

// servePage returns the static page and nothing else: no account data ever
// passes through the unauthenticated resource route.
func (p *Plugin) servePage() managementResponse {
	if len(p.page) == 0 {
		return managementResponse{StatusCode: http.StatusServiceUnavailable, Headers: http.Header{
			"Content-Type":  {"text/plain; charset=utf-8"},
			"Cache-Control": {"no-store"},
		}, Body: []byte("NewAPI Meter page was not built into this library.\n")}
	}
	return managementResponse{StatusCode: http.StatusOK, Headers: http.Header{
		"Content-Type":            {"text/html; charset=utf-8"},
		"Cache-Control":           {"no-store"},
		"X-Content-Type-Options":  {"nosniff"},
		"Referrer-Policy":         {"no-referrer"},
		"Content-Security-Policy": {webui.CSP(p.page)},
	}, Body: p.page}
}

func jsonResponse(code int, v any) managementResponse {
	b, err := json.Marshal(v)
	if err != nil {
		code, b = http.StatusInternalServerError, []byte(`{"error":{"code":"internal"}}`)
	}
	return managementResponse{StatusCode: code, Headers: http.Header{
		"Content-Type":           {"application/json; charset=utf-8"},
		"Cache-Control":          {"no-store"},
		"X-Content-Type-Options": {"nosniff"},
	}, Body: b}
}

type errorBody struct {
	Code   string `json:"code"`
	Field  string `json:"field,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Path is the data file for data-file errors, so the user can inspect or restore it.
	Path string `json:"path,omitempty"`
}

func apiError(code int, body errorBody) managementResponse {
	return jsonResponse(code, map[string]errorBody{"error": body})
}

// storeFailure maps store errors onto responses.
func (p *Plugin) storeFailure(err error) managementResponse {
	var fe *store.FileError
	var ve *store.ValidationError
	switch {
	case errors.As(err, &ve):
		return apiError(http.StatusBadRequest, errorBody{Code: "validation_failed", Field: ve.Field, Reason: ve.Reason})
	case errors.Is(err, store.ErrNotFound):
		return apiError(http.StatusNotFound, errorBody{Code: "account_not_found"})
	case errors.As(err, &fe):
		p.log.Error("data file refused", "error_kind", string(fe.Kind), "path", fe.Path)
		return apiError(http.StatusServiceUnavailable, errorBody{Code: string(fe.Kind), Path: fe.Path})
	}
	p.log.Error("data file operation failed")
	return apiError(http.StatusServiceUnavailable, errorBody{Code: string(store.FileUnavailable)})
}

// accountView is an account as returned to management clients: secrets are
// only ever masked.
type accountView struct {
	ID                  string           `json:"id"`
	Order               int              `json:"order"`
	Name                string           `json:"name"`
	Source              store.Source     `json:"source"`
	BaseURL             string           `json:"base_url"`
	CredentialMasked    string           `json:"credential_masked"`
	CredentialKind      string           `json:"credential_kind"`
	UserID              string           `json:"user_id"`
	LowBalanceThreshold *float64         `json:"low_balance_threshold"`
	Proxy               store.ProxyMode  `json:"proxy"`
	CustomScript        string           `json:"custom_script"`
	CustomPlaceholders  placeholdersView `json:"custom_placeholders"`
}

type placeholdersView struct {
	BaseURL           string `json:"baseUrl"`
	APIKeyMasked      string `json:"apiKeyMasked"`
	AccessTokenMasked string `json:"accessTokenMasked"`
	UserID            string `json:"userId"`
}

func viewOf(a store.Account) accountView {
	kind := ""
	if a.Credential != "" {
		kind = newapi.KindLegacy
		if a.IsPAT() {
			kind = newapi.KindPAT
		}
	}
	return accountView{
		ID: a.ID, Order: a.Order, Name: a.Name, Source: a.Source, BaseURL: a.BaseURL,
		CredentialMasked: store.Mask(a.Credential), CredentialKind: kind, UserID: a.UserID,
		LowBalanceThreshold: a.LowBalanceThreshold, Proxy: a.Proxy, CustomScript: a.CustomScript,
		CustomPlaceholders: placeholdersView{
			BaseURL:           a.CustomPlaceholders.BaseURL,
			APIKeyMasked:      store.Mask(a.CustomPlaceholders.APIKey),
			AccessTokenMasked: store.Mask(a.CustomPlaceholders.AccessToken),
			UserID:            a.CustomPlaceholders.UserID,
		},
	}
}

func (p *Plugin) listAccounts() managementResponse {
	s, err := p.currentStore()
	if err != nil {
		return p.storeFailure(err)
	}
	accounts, err := s.List()
	if err != nil {
		return p.storeFailure(err)
	}
	views := make([]accountView, 0, len(accounts))
	for _, a := range accounts {
		views = append(views, viewOf(a))
	}
	return jsonResponse(http.StatusOK, map[string]any{"accounts": views, "data_file": s.Path()})
}

func (p *Plugin) createAccount(body []byte) managementResponse {
	s, err := p.currentStore()
	if err != nil {
		return p.storeFailure(err)
	}
	var in store.Input
	if err := json.Unmarshal(body, &in); err != nil {
		return apiError(http.StatusBadRequest, errorBody{Code: "invalid_json"})
	}
	a, err := s.Create(in)
	if err != nil {
		return p.storeFailure(err)
	}
	p.log.Info("account created", "account", a.ID)
	return jsonResponse(http.StatusCreated, map[string]any{"account": viewOf(a)})
}

func (p *Plugin) updateAccount(body []byte) managementResponse {
	s, err := p.currentStore()
	if err != nil {
		return p.storeFailure(err)
	}
	var in struct {
		ID string `json:"id"`
		store.Input
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return apiError(http.StatusBadRequest, errorBody{Code: "invalid_json"})
	}
	if in.ID == "" {
		return apiError(http.StatusBadRequest, errorBody{Code: "validation_failed", Field: "id", Reason: store.ReasonRequired})
	}
	a, err := s.Update(in.ID, in.Input)
	if err != nil {
		return p.storeFailure(err)
	}
	p.forget(a.ID)
	p.log.Info("account updated", "account", a.ID)
	return jsonResponse(http.StatusOK, map[string]any{"account": viewOf(a)})
}

func (p *Plugin) deleteAccount(id string) managementResponse {
	s, err := p.currentStore()
	if err != nil {
		return p.storeFailure(err)
	}
	if id == "" {
		return apiError(http.StatusBadRequest, errorBody{Code: "validation_failed", Field: "id", Reason: store.ReasonRequired})
	}
	if err := s.Delete(id); err != nil {
		return p.storeFailure(err)
	}
	p.forget(id)
	p.log.Info("account deleted", "account", id)
	return jsonResponse(http.StatusOK, map[string]any{"deleted": id})
}

func (p *Plugin) forget(id string) {
	p.overviews.Forget(id)
	p.periods.Forget(id)
	p.customs.Forget(id)
	p.okMu.Lock()
	delete(p.lastOK, id)
	p.okMu.Unlock()
}

// overviewItem is one account's tab data.
type overviewItem struct {
	ID                  string           `json:"id"`
	Name                string           `json:"name"`
	Source              store.Source     `json:"source"`
	BaseURL             string           `json:"base_url"`
	Host                string           `json:"host"`
	CredentialKind      string           `json:"credential_kind"`
	State               status.State     `json:"state"`
	SiteVersion         string           `json:"site_version"`
	Currency            *newapi.Currency `json:"currency"`
	DataExport          *bool            `json:"data_export"`
	Balance             *float64         `json:"balance"`
	TotalUsed           *float64         `json:"total_used"`
	TodaySpend          *float64         `json:"today_spend"`
	RPM                 *float64         `json:"rpm"`
	TPM                 *float64         `json:"tpm"`
	LowBalanceThreshold *float64         `json:"low_balance_threshold"`
	BalanceFailure      *status.Failure  `json:"balance_failure"`
	TodayFailure        *status.Failure  `json:"today_failure"`
	PrimaryFailure      *status.Failure  `json:"primary_failure"`
	Failures            []status.Probe   `json:"failures"`
	FetchedAt           time.Time        `json:"fetched_at"`
	LastSuccessAt       *time.Time       `json:"last_success_at"`
	Cached              bool             `json:"cached"`
	UnsupportedSource   bool             `json:"unsupported_source,omitempty"`
	// Custom carries what only custom-query accounts have.
	Custom *customView `json:"custom,omitempty"`
}

// overview serves GET .../overview. Query: force=1 bypasses the cache; with
// id=<account id> only that account is forced and the rest may come from cache.
func (p *Plugin) overview(ctx context.Context, q url.Values) managementResponse {
	s, err := p.currentStore()
	if err != nil {
		return p.storeFailure(err)
	}
	accounts, err := s.List()
	if err != nil {
		return p.storeFailure(err)
	}
	force := q.Get("force") == "1" || q.Get("force") == "true"
	only := q.Get("id")
	now := p.now()
	items := make([]overviewItem, len(accounts))
	var wg sync.WaitGroup
	for i, a := range accounts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			items[i] = p.overviewOf(ctx, a, force && (only == "" || only == a.ID))
		}()
	}
	wg.Wait()
	// time.Local names itself "Local"; the zone abbreviation (CST, EST, +08…) says more.
	zone, _ := now.Zone()
	return jsonResponse(http.StatusOK, map[string]any{
		"accounts":     items,
		"generated_at": now,
		"today_start":  newapi.LocalMidnight(now),
		"timezone":     zone,
	})
}

func (p *Plugin) overviewOf(ctx context.Context, a store.Account, force bool) overviewItem {
	item := overviewItem{
		ID: a.ID, Name: a.Name, Source: a.Source, BaseURL: a.BaseURL, Host: hostOf(a.BaseURL),
		LowBalanceThreshold: a.LowBalanceThreshold, Failures: []status.Probe{},
	}
	if a.Source == store.SourceCustom {
		return p.customOverviewOf(ctx, a, force, item)
	}
	if a.Source != store.SourceNewAPI {
		item.State = status.NeedsAction
		item.UnsupportedSource = true
		item.FetchedAt = p.now()
		return item
	}
	r, at, hit := p.reading(ctx, a, force)
	sum := newapi.Summarize(r, a.LowBalanceThreshold)
	item.CredentialKind = r.CredentialKind
	if r.Site != nil {
		item.SiteVersion = r.Site.Version
	}
	item.State, item.Currency, item.DataExport = sum.State, sum.Currency, sum.DataExport
	item.Balance, item.TotalUsed, item.TodaySpend, item.RPM, item.TPM = sum.Balance, sum.TotalUsed, sum.TodaySpend, sum.RPM, sum.TPM
	item.BalanceFailure, item.TodayFailure, item.PrimaryFailure = sum.BalanceFailure, sum.TodayFailure, sum.Primary
	if sum.Failures != nil {
		item.Failures = sum.Failures
	}
	item.FetchedAt, item.Cached = at, hit
	p.okMu.Lock()
	if t, ok := p.lastOK[a.ID]; ok {
		item.LastSuccessAt = &t
	}
	p.okMu.Unlock()
	return item
}

// reading is an account's today query, shared by the overview and today's
// period of the detail through one cache entry.
func (p *Plugin) reading(ctx context.Context, a store.Account, force bool) (newapi.Reading, time.Time, bool) {
	key := cache.Key{Account: a.ID, Query: "overview", Fingerprint: fingerprint(a)}
	return p.overviews.Get(key, force, func() newapi.Reading {
		r := p.source.Fetch(ctx, a)
		p.logProbes(a.ID, r.Probes)
		if r.Succeeded() {
			p.okMu.Lock()
			p.lastOK[a.ID] = r.FetchedAt
			p.okMu.Unlock()
		}
		return r
	})
}

// detailResponse is one built-in account's figures for one period.
type detailResponse struct {
	ID            string           `json:"id"`
	Period        periodView       `json:"period"`
	Timezone      string           `json:"timezone"`
	Currency      *newapi.Currency `json:"currency"`
	DataExport    *bool            `json:"data_export"`
	Spend         *float64         `json:"spend"`
	SpendFailure  *status.Failure  `json:"spend_failure"`
	Usage         *newapi.Usage    `json:"usage"`
	UsageFailure  *status.Failure  `json:"usage_failure"`
	UsageDisabled bool             `json:"usage_disabled"`
	FetchedAt     time.Time        `json:"fetched_at"`
	Cached        bool             `json:"cached"`
}

type periodView struct {
	Preset      string    `json:"preset"`
	StartDate   string    `json:"start_date"`
	EndDate     string    `json:"end_date"`
	Days        int       `json:"days"`
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	Granularity string    `json:"granularity"`
	IncludesNow bool      `json:"includes_now"`
	Minutes     float64   `json:"minutes"`
}

// detail serves GET .../detail. Query: id; period = today (default) |
// yesterday | 7d | 30d | custom, with start and end (YYYY-MM-DD, both
// included) for custom; force=1 bypasses the cache. Period queries never
// change the account status: today's period reads the overview's cached
// query, other periods have their own cache entries.
func (p *Plugin) detail(ctx context.Context, q url.Values) managementResponse {
	s, err := p.currentStore()
	if err != nil {
		return p.storeFailure(err)
	}
	id := q.Get("id")
	if id == "" {
		return apiError(http.StatusBadRequest, errorBody{Code: "validation_failed", Field: "id", Reason: store.ReasonRequired})
	}
	a, err := s.Get(id)
	if err != nil {
		return p.storeFailure(err)
	}
	if a.Source != store.SourceNewAPI {
		return apiError(http.StatusBadRequest, errorBody{Code: "validation_failed", Field: "id", Reason: store.ReasonUnsupported})
	}
	now := p.now()
	per, err := newapi.ResolvePeriod(now, q.Get("period"), q.Get("start"), q.Get("end"))
	if err != nil {
		var pe *newapi.PeriodError
		errors.As(err, &pe)
		return apiError(http.StatusBadRequest, errorBody{Code: "validation_failed", Field: pe.Field, Reason: pe.Reason})
	}
	force := q.Get("force") == "1" || q.Get("force") == "true"

	var (
		sum newapi.PeriodSummary
		at  time.Time
		hit bool
	)
	if per.IsToday(now) {
		r, rat, rhit := p.reading(ctx, a, force)
		if !r.TodayStart.Equal(per.Start) {
			// The cached reading is from before local midnight.
			r, rat, rhit = p.reading(ctx, a, true)
		}
		sum, at, hit = newapi.SummarizePeriod(r.TodayPeriod(), per), rat, rhit
	} else {
		key := cache.Key{Account: a.ID, Query: "period:" + per.StartDate + ":" + per.EndDate, Fingerprint: fingerprint(a)}
		sum, at, hit = p.periods.Get(key, force, func() newapi.PeriodSummary {
			r := p.source.FetchPeriod(ctx, a, per)
			p.logProbes(a.ID, r.Probes)
			return newapi.SummarizePeriod(r, per)
		})
	}
	zone, _ := now.Zone()
	return jsonResponse(http.StatusOK, detailResponse{
		ID: a.ID,
		Period: periodView{
			Preset: per.Preset, StartDate: per.StartDate, EndDate: per.EndDate, Days: per.Days,
			Start: per.Start, End: per.End, Granularity: per.Granularity,
			IncludesNow: sum.IncludesNow, Minutes: sum.Minutes,
		},
		Timezone: zone, Currency: sum.Currency, DataExport: sum.DataExport,
		Spend: sum.Spend, SpendFailure: sum.SpendFailure,
		Usage: sum.Usage, UsageFailure: sum.UsageFailure, UsageDisabled: sum.UsageDisabled,
		FetchedAt: at, Cached: hit,
	})
}

// logProbes records failed endpoints by class only: no URL query, header,
// credential or response body is logged.
func (p *Plugin) logProbes(id string, probes []status.Probe) {
	for _, pr := range probes {
		if pr.Failure != nil {
			p.log.Warn("site query failed", "account", id, "endpoint", pr.Name,
				"failure", string(pr.Failure.Code), "diagnostic", pr.Failure.Diagnostic, "http_status", pr.Failure.HTTPStatus)
		}
	}
}

// fingerprint covers every fetch input; the credential only enters as a hash.
func fingerprint(a store.Account) string {
	h := sha256.New()
	for _, s := range []string{string(a.Source), a.BaseURL, a.Credential, a.UserID, string(a.Proxy)} {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func hostOf(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	return u.Host
}

// connectionTest is the answer of POST .../accounts/test.
type connectionTest struct {
	Source store.Source `json:"source"`
	// OK: every query that counts answered.
	OK             bool             `json:"ok"`
	State          status.State     `json:"state"`
	CredentialKind string           `json:"credential_kind"`
	SiteVersion    string           `json:"site_version"`
	Currency       *newapi.Currency `json:"currency"`
	DataExport     *bool            `json:"data_export"`
	Failure        *status.Failure  `json:"failure"`
	Failures       []status.Probe   `json:"failures"`
	// Custom is the extraction result or script error of a custom query.
	Custom *customTest `json:"custom,omitempty"`
}

// testConnection serves POST .../accounts/test: it queries the site once with
// unsaved form values. With "id", blank or masked secrets mean that account's
// stored ones. Nothing is cached and the data file is only read, never written.
func (p *Plugin) testConnection(ctx context.Context, body []byte) managementResponse {
	var in struct {
		ID string `json:"id"`
		store.Input
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return apiError(http.StatusBadRequest, errorBody{Code: "invalid_json"})
	}
	var prev *store.Account
	if in.ID != "" {
		s, err := p.currentStore()
		if err != nil {
			return p.storeFailure(err)
		}
		a, err := s.Get(in.ID)
		if err != nil {
			return p.storeFailure(err)
		}
		prev = &a
	}
	a, err := store.Resolve(in.Input, prev)
	if err != nil {
		return p.storeFailure(err)
	}
	out := connectionTest{Source: a.Source, Failures: []status.Probe{}}
	switch a.Source {
	case store.SourceNewAPI:
		c := p.source.Check(ctx, a)
		out.OK, out.State, out.CredentialKind = c.Failure == nil, c.State, c.CredentialKind
		out.SiteVersion, out.Currency, out.DataExport = c.SiteVersion, c.Currency, c.DataExport
		out.Failure = c.Failure
		if c.Failures != nil {
			out.Failures = c.Failures
		}
	case store.SourceCustom:
		r := p.custom.Run(ctx, a)
		sum := customquery.Summarize(r, nil)
		out.OK, out.State, out.Currency, out.Failure = sum.Failure == nil, sum.State, sum.Currency, sum.Failure
		if sum.Failure != nil {
			out.Failures = sum.Probes
		}
		out.Custom = &customTest{Balance: sum.Balance, TotalUsed: sum.TotalUsed, TodaySpend: sum.TodaySpend, customView: customViewOf(r, sum)}
	default:
		// store.Resolve accepts only sources this build can query.
		return apiError(http.StatusBadRequest, errorBody{Code: "validation_failed", Field: "source", Reason: store.ReasonUnsupported})
	}
	return jsonResponse(http.StatusOK, out)
}
