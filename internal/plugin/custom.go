package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/cache"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/customquery"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/newapi"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/status"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/store"
)

// customView is the part of a custom-query reading that built-in accounts
// do not have. Every string in it comes from the script, with the
// substituted apiKey and accessToken masked.
type customView struct {
	// BalanceBasis is "sum" (every plan has the same unit) or "first";
	// empty for a single item.
	BalanceBasis   customquery.Basis `json:"balance_basis,omitempty"`
	TotalQuota     *float64          `json:"total_quota"`
	Plans          []planView        `json:"plans"`
	InvalidMessage string            `json:"invalid_message,omitempty"`
	// ScriptError is the engine's or script's error text, masked; ScriptReason
	// is a failure the plugin explains with a code the page words itself.
	ScriptError  string              `json:"script_error,omitempty"`
	ScriptReason *customquery.Reason `json:"script_reason,omitempty"`
}

// planView is one extractor item.
type planView struct {
	PlanName       string           `json:"plan_name"`
	Extra          string           `json:"extra"`
	Remaining      *float64         `json:"remaining"`
	Used           *float64         `json:"used"`
	Total          *float64         `json:"total"`
	TodaySpend     *float64         `json:"today_spend"`
	Unit           string           `json:"unit"`
	Currency       *newapi.Currency `json:"currency"`
	IsValid        *bool            `json:"is_valid"`
	InvalidMessage string           `json:"invalid_message,omitempty"`
}

// customTest is the custom part of a test-script answer.
type customTest struct {
	Balance    *float64 `json:"balance"`
	TotalUsed  *float64 `json:"total_used"`
	TodaySpend *float64 `json:"today_spend"`
	// TodaySpendCurrency is the unit of the plan that provided today_spend.
	TodaySpendCurrency *newapi.Currency `json:"today_spend_currency"`
	customView
}

func customViewOf(r customquery.Result, s customquery.Summary) customView {
	v := customView{
		BalanceBasis:   s.Basis,
		TotalQuota:     s.TotalQuota,
		Plans:          make([]planView, 0, len(r.Items)),
		InvalidMessage: s.InvalidMessage,
		ScriptError:    r.Message,
		ScriptReason:   r.Reason,
	}
	for _, it := range r.Items {
		c := customquery.Currency(it.Unit)
		v.Plans = append(v.Plans, planView{
			PlanName: it.PlanName, Extra: it.Extra,
			Remaining: it.Remaining, Used: it.Used, Total: it.Total, TodaySpend: it.TodaySpend,
			Unit: it.Unit, Currency: &c, IsValid: it.IsValid, InvalidMessage: it.InvalidMessage,
		})
	}
	return v
}

// customOverviewOf fills a custom-query account's tab data. The query is
// cached and coalesced like a built-in one; the account status comes from
// the same rules (status.Decide) with the query as its one probe.
func (p *Plugin) customOverviewOf(ctx context.Context, a store.Account, force bool, item overviewItem) overviewItem {
	key := cache.Key{Account: a.ID, Query: "custom", Fingerprint: customFingerprint(a)}
	r, at, hit := p.customs.Get(key, force, func() customquery.Result {
		r := p.custom.Run(ctx, a)
		if f := customquery.Summarize(r, nil).Failure; f != nil {
			// Class and diagnostic only: never the message, script or values.
			p.log.Warn("custom query failed", "account", a.ID, "failure", string(f.Code), "diagnostic", f.Diagnostic, "http_status", f.HTTPStatus)
		} else {
			p.okMu.Lock()
			p.lastOK[a.ID] = r.FetchedAt
			p.okMu.Unlock()
		}
		return r
	})
	sum := customquery.Summarize(r, a.LowBalanceThreshold)
	item.State, item.Currency = sum.State, sum.Currency
	item.Balance, item.TotalUsed, item.TodaySpend, item.TodaySpendCurrency = sum.Balance, sum.TotalUsed, sum.TodaySpend, sum.TodayCurrency
	if sum.State == status.NeedsAction {
		item.PrimaryFailure = sum.Failure
		item.Failures = sum.Probes
	}
	v := customViewOf(r, sum)
	item.Custom = &v
	item.FetchedAt, item.Cached = at, hit
	p.okMu.Lock()
	if t, ok := p.lastOK[a.ID]; ok {
		item.LastSuccessAt = &t
	}
	p.okMu.Unlock()
	return item
}

// customFingerprint covers every input of a custom query; secrets only enter
// as a hash.
func customFingerprint(a store.Account) string {
	h := sha256.New()
	ph := a.CustomPlaceholders
	for _, s := range []string{string(a.Source), a.CustomScript, ph.BaseURL, ph.APIKey, ph.AccessToken, ph.UserID, string(a.Proxy)} {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
