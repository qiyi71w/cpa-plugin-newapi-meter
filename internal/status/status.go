// Package status is the one place that turns upstream results into failure
// classes and decides an account's status. Every data source reuses it.
package status

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime"
	"strconv"
	"strings"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/outbound"
)

// Code is a failure class shown to the user.
type Code string

const (
	// SiteUnreachable: network, DNS, TLS error or timeout.
	SiteUnreachable Code = "site_unreachable"
	// LegacyTokenRetired: ACCESS_TOKEN_LEGACY_RETIRED.
	LegacyTokenRetired Code = "legacy_token_retired"
	// PATScopeDenied: ACCESS_TOKEN_SCOPE_DENIED or ACCESS_TOKEN_ROUTE_UNDECLARED.
	PATScopeDenied Code = "pat_scope_denied"
	// CredentialInvalid: 401 or another authentication refusal.
	CredentialInvalid Code = "credential_invalid"
	// SiteBlocked: the site answered with an HTML page instead of JSON.
	SiteBlocked Code = "site_blocked"
	// SiteError: HTTP 5xx.
	SiteError Code = "site_error"
	// BadResponse: anything else that cannot be used, including JSON success:false.
	BadResponse Code = "bad_response"
	// ScriptError: a custom-query script failed to evaluate, described a
	// request the plugin will not send, or returned an unusable result.
	ScriptError Code = "script_error"
	// QueryTimeout: a custom query (request plus script) ran past its time limit.
	QueryTimeout Code = "query_timeout"
	// ReportedInvalid: a custom-query script returned isValid: false.
	ReportedInvalid Code = "reported_invalid"
)

// Failure is one classified failure. Diagnostic is a short machine code
// (an upstream error code, HTTP_<status> or a transport kind) safe to show
// and log: it never contains response bodies or credentials.
type Failure struct {
	Code       Code   `json:"code"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Diagnostic string `json:"diagnostic"`
}

var upstreamCodes = []struct {
	token string
	code  Code
}{
	{"ACCESS_TOKEN_LEGACY_RETIRED", LegacyTokenRetired},
	{"ACCESS_TOKEN_SCOPE_DENIED", PATScopeDenied},
	{"ACCESS_TOKEN_ROUTE_UNDECLARED", PATScopeDenied},
}

// ClassifyHTTP classifies a raw outbound result. It returns nil when the
// upstream answered 2xx with a JSON body; JSON envelope semantics are left to
// the caller (see ClassifyNewAPI).
func ClassifyHTTP(resp *outbound.Response, err error) *Failure {
	if err != nil || resp == nil {
		var oe *outbound.Error
		if errors.As(err, &oe) {
			if oe.Kind == outbound.ErrTooLarge {
				return &Failure{Code: BadResponse, Diagnostic: "RESPONSE_TOO_LARGE"}
			}
			return &Failure{Code: SiteUnreachable, Diagnostic: "NETWORK_" + strings.ToUpper(string(oe.Kind))}
		}
		return &Failure{Code: SiteUnreachable, Diagnostic: "NETWORK_ERROR"}
	}
	st := resp.StatusCode
	if st < 200 || st >= 300 || successFalse(resp.Body) {
		for _, u := range upstreamCodes {
			if bytes.Contains(resp.Body, []byte(u.token)) {
				return &Failure{Code: u.code, HTTPStatus: st, Diagnostic: u.token}
			}
		}
	}
	switch {
	case st >= 500:
		return &Failure{Code: SiteError, HTTPStatus: st, Diagnostic: httpDiag(st)}
	case st == 401:
		return &Failure{Code: CredentialInvalid, HTTPStatus: st, Diagnostic: httpDiag(st)}
	case isHTML(resp):
		return &Failure{Code: SiteBlocked, HTTPStatus: st, Diagnostic: "HTML_RESPONSE"}
	case st == 403:
		return &Failure{Code: CredentialInvalid, HTTPStatus: st, Diagnostic: httpDiag(st)}
	case st < 200 || st >= 300:
		return &Failure{Code: BadResponse, HTTPStatus: st, Diagnostic: httpDiag(st)}
	case !json.Valid(resp.Body):
		return &Failure{Code: BadResponse, HTTPStatus: st, Diagnostic: "INVALID_JSON"}
	}
	return nil
}

// ClassifyNewAPI classifies a new-api style response ({success, message, data})
// and returns its data member on success. A JSON success:false that is not in
// one of the classes above is BadResponse.
func ClassifyNewAPI(resp *outbound.Response, err error) (json.RawMessage, *Failure) {
	if f := ClassifyHTTP(resp, err); f != nil {
		return nil, f
	}
	var env struct {
		Success *bool           `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if json.Unmarshal(resp.Body, &env) != nil {
		return nil, &Failure{Code: BadResponse, HTTPStatus: resp.StatusCode, Diagnostic: "INVALID_JSON"}
	}
	if env.Success != nil && !*env.Success {
		return nil, &Failure{Code: BadResponse, HTTPStatus: resp.StatusCode, Diagnostic: "SUCCESS_FALSE"}
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return nil, &Failure{Code: BadResponse, HTTPStatus: resp.StatusCode, Diagnostic: "MISSING_DATA"}
	}
	return env.Data, nil
}

func isHTML(resp *outbound.Response) bool {
	if mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err == nil && (mt == "text/html" || mt == "application/xhtml+xml") {
		return true
	}
	b := bytes.TrimLeft(resp.Body, " \t\r\n\ufeff")
	return len(b) > 0 && b[0] == '<'
}

func httpDiag(st int) string { return "HTTP_" + strconv.Itoa(st) }

// successFalse reports a JSON object whose top-level success is false.
func successFalse(body []byte) bool {
	var env struct {
		Success *bool `json:"success"`
	}
	return json.Unmarshal(body, &env) == nil && env.Success != nil && !*env.Success
}

// State is an account's status.
type State string

const (
	OK          State = "ok"
	Low         State = "low"
	Partial     State = "partial"
	NeedsAction State = "needs_action"
)

// Probe is the outcome of one query that feeds the account status.
// Credentialed marks queries that need the account credential; public
// queries (such as /api/status) succeed with any credential, so they never
// make an account need action on their own. Callers omit queries that must
// not count (for example /api/data/self when enable_data_export is false).
type Probe struct {
	Name         string   `json:"endpoint"`
	Credentialed bool     `json:"credentialed"`
	Failure      *Failure `json:"failure,omitempty"`
}

// Decide applies the account-status rules:
//   - NeedsAction: there are credentialed probes and every one failed;
//   - Partial: some probe failed (a partial account skips the threshold);
//   - Low: balance is known and below threshold;
//   - OK: otherwise.
func Decide(probes []Probe, balance, threshold *float64) State {
	credentialed, credFailed, failed := 0, 0, 0
	for _, p := range probes {
		if p.Failure != nil {
			failed++
		}
		if p.Credentialed {
			credentialed++
			if p.Failure != nil {
				credFailed++
			}
		}
	}
	switch {
	case credentialed > 0 && credFailed == credentialed:
		return NeedsAction
	case failed > 0:
		return Partial
	case balance != nil && threshold != nil && *balance < *threshold:
		return Low
	}
	return OK
}

// priority orders failures by how directly they tell the user what to fix.
var priority = map[Code]int{
	LegacyTokenRetired: 0,
	PATScopeDenied:     1,
	CredentialInvalid:  2,
	SiteBlocked:        3,
	SiteUnreachable:    4,
	SiteError:          5,
	BadResponse:        6,
	ReportedInvalid:    7,
	ScriptError:        8,
	QueryTimeout:       9,
}

// Primary returns the most actionable failure among probes (credentialed
// first), or nil when none failed. Use it for the needs-action headline.
func Primary(probes []Probe) *Failure {
	var best *Failure
	bestRank := 1 << 30
	for _, p := range probes {
		if p.Failure == nil {
			continue
		}
		rank := priority[p.Failure.Code]
		if !p.Credentialed {
			rank += 100
		}
		if rank < bestRank {
			best, bestRank = p.Failure, rank
		}
	}
	return best
}
