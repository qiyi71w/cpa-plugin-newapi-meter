// Package newapi is the built-in new-api data source: it reads an account's
// today figures from four site endpoints and converts quota to the site's
// display currency.
package newapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/outbound"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/status"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/store"
)

// RequestTimeout bounds each request of the built-in source.
const RequestTimeout = 15 * time.Second

// Endpoints read for the overview.
const (
	EndpointStatus = "/api/status"        // public
	EndpointSelf   = "/api/user/self"     // credentialed
	EndpointStat   = "/api/log/self/stat" // credentialed
	EndpointData   = "/api/data/self"     // credentialed
)

// Credential kinds.
const (
	KindPAT    = "pat"
	KindLegacy = "legacy"
)

// Currency is an account's display currency.
type Currency struct {
	// Code is USD, CNY, CUSTOM or TOKENS.
	Code   string `json:"code"`
	Symbol string `json:"symbol"`
}

// Site holds what /api/status tells about money and features.
type Site struct {
	Version  string
	Currency Currency
	// DataExport is enable_data_export; nil when the site does not report it.
	DataExport   *bool
	quotaPerUnit float64
	rate         float64
}

// Convert turns a quota amount into the display currency, as the site's own
// frontend does: quota / quota_per_unit is USD; CNY and CUSTOM multiply by
// their exchange rate; TOKENS shows the quota itself.
func (s *Site) Convert(quota float64) float64 {
	if s.Currency.Code == "TOKENS" {
		return quota
	}
	return quota / s.quotaPerUnit * s.rate
}

// Reading is the raw result of one overview fetch, before the account's
// threshold is applied. Amounts are in quota units.
type Reading struct {
	CredentialKind string
	// Site is nil when /api/status failed.
	Site *Site
	// Probes lists the four endpoints in a fixed order with their failures.
	Probes     []status.Probe
	Balance    *float64 // user/self quota
	TotalUsed  *float64 // user/self used_quota
	TodayQuota *float64 // log/self/stat quota for today
	RPM, TPM   *float64 // log/self/stat realtime values (last 60 s)
	TodayStart time.Time
	FetchedAt  time.Time
}

// Source fetches readings for built-in accounts.
type Source struct {
	HTTP outbound.Doer
	// Now returns the current time in CPA's local zone; "today" starts at its
	// local midnight. Tests inject a fixed zone.
	Now func() time.Time
	// Timeout bounds each request; zero means RequestTimeout.
	Timeout   time.Duration
	UserAgent string
}

// LocalMidnight is the start of the local day containing t, in t's zone.
func LocalMidnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// Fetch queries the four endpoints concurrently. It never returns an error:
// every failure is classified into the matching probe.
func (s *Source) Fetch(ctx context.Context, a store.Account) Reading {
	now := s.Now()
	start := LocalMidnight(now)
	r := Reading{CredentialKind: KindLegacy, TodayStart: start, FetchedAt: now}
	if a.IsPAT() {
		r.CredentialKind = KindPAT
	}
	q := url.Values{}
	q.Set("start_timestamp", strconv.FormatInt(start.Unix(), 10))
	q.Set("end_timestamp", strconv.FormatInt(now.Unix(), 10))
	statQ := url.Values{"type": {"2"}}
	for k, v := range q {
		statQ[k] = v
	}

	var (
		wg                                       sync.WaitGroup
		statusData, selfData, statData, rowsData json.RawMessage
		fStatus, fSelf, fStat, fData             *status.Failure
	)
	wg.Add(4)
	go func() { defer wg.Done(); statusData, fStatus = s.get(ctx, a, EndpointStatus, "", false) }()
	go func() { defer wg.Done(); selfData, fSelf = s.get(ctx, a, EndpointSelf, "", true) }()
	go func() { defer wg.Done(); statData, fStat = s.get(ctx, a, EndpointStat, statQ.Encode(), true) }()
	go func() { defer wg.Done(); rowsData, fData = s.get(ctx, a, EndpointData, q.Encode(), true) }()
	wg.Wait()

	if fStatus == nil {
		r.Site, fStatus = parseSite(statusData)
	}
	if fSelf == nil {
		var v struct {
			Quota     *float64 `json:"quota"`
			UsedQuota *float64 `json:"used_quota"`
		}
		if json.Unmarshal(selfData, &v) != nil || v.Quota == nil || v.UsedQuota == nil {
			fSelf = missingFields()
		} else {
			r.Balance, r.TotalUsed = v.Quota, v.UsedQuota
		}
	}
	if fStat == nil {
		var v struct {
			Quota *float64 `json:"quota"`
			RPM   *float64 `json:"rpm"`
			TPM   *float64 `json:"tpm"`
		}
		if json.Unmarshal(statData, &v) != nil || v.Quota == nil {
			fStat = missingFields()
		} else {
			r.TodayQuota, r.RPM, r.TPM = v.Quota, v.RPM, v.TPM
		}
	}
	if fData == nil {
		var rows []json.RawMessage
		if json.Unmarshal(rowsData, &rows) != nil {
			fData = missingFields()
		}
	}
	r.Probes = []status.Probe{
		{Name: EndpointStatus, Failure: fStatus},
		{Name: EndpointSelf, Credentialed: true, Failure: fSelf},
		{Name: EndpointStat, Credentialed: true, Failure: fStat},
		{Name: EndpointData, Credentialed: true, Failure: fData},
	}
	return r
}

func missingFields() *status.Failure {
	return &status.Failure{Code: status.BadResponse, HTTPStatus: http.StatusOK, Diagnostic: "MISSING_FIELDS"}
}

func (s *Source) get(ctx context.Context, a store.Account, path, rawQuery string, credentialed bool) (json.RawMessage, *status.Failure) {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = RequestTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	u := a.BaseURL + path
	if rawQuery != "" {
		u += "?" + rawQuery
	}
	h := http.Header{}
	h.Set("Accept", "application/json")
	if s.UserAgent != "" {
		h.Set("User-Agent", s.UserAgent)
	}
	if credentialed {
		h.Set("Authorization", "Bearer "+a.Credential)
		if !a.IsPAT() {
			h.Set("New-Api-User", a.UserID)
		}
	}
	resp, err := s.HTTP.Do(ctx, outbound.Request{Method: http.MethodGet, URL: u, Header: h, Proxy: outbound.Proxy(a.Proxy)})
	return status.ClassifyNewAPI(resp, err)
}

func parseSite(data json.RawMessage) (*Site, *status.Failure) {
	var v struct {
		QuotaPerUnit               *float64 `json:"quota_per_unit"`
		QuotaDisplayType           *string  `json:"quota_display_type"`
		DisplayInCurrency          *bool    `json:"display_in_currency"`
		USDExchangeRate            *float64 `json:"usd_exchange_rate"`
		CustomCurrencySymbol       *string  `json:"custom_currency_symbol"`
		CustomCurrencyExchangeRate *float64 `json:"custom_currency_exchange_rate"`
		Version                    *string  `json:"version"`
		EnableDataExport           *bool    `json:"enable_data_export"`
	}
	if json.Unmarshal(data, &v) != nil {
		return nil, missingFields()
	}
	s := &Site{DataExport: v.EnableDataExport, rate: 1}
	if v.Version != nil {
		s.Version = *v.Version
	}
	display := "USD"
	switch {
	case v.QuotaDisplayType != nil && *v.QuotaDisplayType != "":
		display = *v.QuotaDisplayType
	case v.DisplayInCurrency != nil && !*v.DisplayInCurrency:
		// Older sites have no quota_display_type; display_in_currency=false means tokens.
		display = "TOKENS"
	}
	positive := func(p *float64) bool { return p != nil && *p > 0 }
	switch display {
	case "TOKENS":
		s.Currency = Currency{Code: "TOKENS"}
		return s, nil
	case "USD":
		s.Currency = Currency{Code: "USD", Symbol: "$"}
	case "CNY":
		if !positive(v.USDExchangeRate) {
			return nil, missingFields()
		}
		s.Currency, s.rate = Currency{Code: "CNY", Symbol: "¥"}, *v.USDExchangeRate
	case "CUSTOM":
		if !positive(v.CustomCurrencyExchangeRate) {
			return nil, missingFields()
		}
		symbol := "¤"
		if v.CustomCurrencySymbol != nil && *v.CustomCurrencySymbol != "" {
			symbol = *v.CustomCurrencySymbol
		}
		s.Currency, s.rate = Currency{Code: "CUSTOM", Symbol: symbol}, *v.CustomCurrencyExchangeRate
	default:
		return nil, &status.Failure{Code: status.BadResponse, HTTPStatus: http.StatusOK, Diagnostic: "UNKNOWN_DISPLAY_TYPE"}
	}
	if !positive(v.QuotaPerUnit) {
		return nil, missingFields()
	}
	s.quotaPerUnit = *v.QuotaPerUnit
	return s, nil
}

// Summary is a Reading judged against the account's threshold, in display units.
type Summary struct {
	State      status.State
	Currency   *Currency
	Balance    *float64
	TotalUsed  *float64
	TodaySpend *float64
	RPM, TPM   *float64
	// BalanceFailure explains why Balance/TotalUsed are unavailable.
	BalanceFailure *status.Failure
	// TodayFailure explains why TodaySpend (and RPM/TPM) are unavailable.
	TodayFailure *status.Failure
	// Failures lists the failed probes that count toward the status.
	Failures []status.Probe
	// Primary is the headline failure for a needs-action account.
	Primary    *status.Failure
	DataExport *bool
}

// Summarize applies the account-status rules to r. /api/data/self only
// counts while the site does not report enable_data_export=false: a site
// that turned its dashboard off still answers that endpoint successfully,
// so its result says nothing then.
func Summarize(r Reading, threshold *float64) Summary {
	var out Summary
	probes := make([]status.Probe, 0, len(r.Probes))
	var fStatus, fSelf, fStat *status.Failure
	for _, p := range r.Probes {
		switch p.Name {
		case EndpointStatus:
			fStatus = p.Failure
		case EndpointSelf:
			fSelf = p.Failure
		case EndpointStat:
			fStat = p.Failure
		case EndpointData:
			if r.Site != nil && r.Site.DataExport != nil && !*r.Site.DataExport {
				continue
			}
		}
		probes = append(probes, p)
		if p.Failure != nil {
			out.Failures = append(out.Failures, p)
		}
	}
	if r.Site != nil {
		c := r.Site.Currency
		out.Currency = &c
		out.DataExport = r.Site.DataExport
	}
	conv := func(q *float64) *float64 {
		if q == nil || r.Site == nil {
			return nil
		}
		v := r.Site.Convert(*q)
		return &v
	}
	out.Balance, out.TotalUsed = conv(r.Balance), conv(r.TotalUsed)
	out.TodaySpend = conv(r.TodayQuota)
	out.RPM, out.TPM = r.RPM, r.TPM
	switch {
	case fSelf != nil:
		out.BalanceFailure = fSelf
	case fStatus != nil:
		out.BalanceFailure = fStatus
	}
	switch {
	case fStat != nil:
		out.TodayFailure = fStat
	case fStatus != nil:
		out.TodayFailure = fStatus
	}
	out.State = status.Decide(probes, out.Balance, threshold)
	if out.State == status.NeedsAction {
		out.Primary = status.Primary(probes)
	}
	return out
}

// Succeeded reports whether any credentialed endpoint answered, which is
// what "last successful fetch" means for an account.
func (r Reading) Succeeded() bool {
	for _, p := range r.Probes {
		if p.Credentialed && p.Failure == nil {
			return true
		}
	}
	return false
}

// Check is the outcome of a connection test: what the site tells about an
// account that may not be saved yet.
type Check struct {
	CredentialKind string
	// SiteVersion and Currency are empty/nil when /api/status failed.
	SiteVersion string
	Currency    *Currency
	DataExport  *bool
	// State is the account status the reading would give, judged without a
	// threshold (so never Low).
	State status.State
	// Failure is the most actionable failure, nil when every counted query
	// answered; Failures lists each failed query.
	Failure  *status.Failure
	Failures []status.Probe
}

// Check queries the site once for a, bypassing any cache, and judges the
// result with the same rules as the account status.
func (s *Source) Check(ctx context.Context, a store.Account) Check {
	r := s.Fetch(ctx, a)
	sum := Summarize(r, nil)
	c := Check{
		CredentialKind: r.CredentialKind,
		Currency:       sum.Currency,
		DataExport:     sum.DataExport,
		State:          sum.State,
		Failure:        status.Primary(sum.Failures),
		Failures:       sum.Failures,
	}
	if r.Site != nil {
		c.SiteVersion = r.Site.Version
	}
	return c
}
