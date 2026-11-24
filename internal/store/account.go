package store

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
)

// SchemaVersion is the data-file format this build reads and writes. Version 1
// already carries every account field later sources need, so it is not migrated.
const SchemaVersion = 1

// Source is an account's data source.
type Source string

const (
	SourceNewAPI Source = "newapi"
	SourceCustom Source = "custom"
)

// ProxyMode selects how an account's outbound requests leave the host.
type ProxyMode string

const (
	ProxyFollow ProxyMode = "follow"
	ProxyDirect ProxyMode = "direct"
)

// PATPrefix marks a personal access token; any other credential is a legacy
// access token that also needs the numeric user ID.
const PATPrefix = "nap_"

// Placeholders are the custom-query placeholder values substituted into the
// script ({{baseUrl}}, {{apiKey}}, {{accessToken}}, {{userId}}).
type Placeholders struct {
	BaseURL     string `json:"baseUrl"`
	APIKey      string `json:"apiKey"`
	AccessToken string `json:"accessToken"`
	UserID      string `json:"userId"`
}

// Account is one v1 account record as stored in the data file.
type Account struct {
	ID                  string       `json:"id"`
	Order               int          `json:"order"`
	Name                string       `json:"name"`
	Source              Source       `json:"source"`
	BaseURL             string       `json:"base_url"`
	Credential          string       `json:"credential"`
	UserID              string       `json:"user_id"`
	LowBalanceThreshold *float64     `json:"low_balance_threshold"`
	Proxy               ProxyMode    `json:"proxy"`
	CustomScript        string       `json:"custom_script"`
	CustomPlaceholders  Placeholders `json:"custom_placeholders"`
}

// IsPAT reports whether the account credential is a personal access token.
func (a Account) IsPAT() bool { return strings.HasPrefix(a.Credential, PATPrefix) }

// Input is the writable part of an account submitted by a management client.
// Blank secrets (Credential, APIKey, AccessToken) keep the stored value on update.
type Input struct {
	Name                string       `json:"name"`
	Source              Source       `json:"source"`
	BaseURL             string       `json:"base_url"`
	Credential          string       `json:"credential"`
	UserID              string       `json:"user_id"`
	LowBalanceThreshold *float64     `json:"low_balance_threshold"`
	Proxy               ProxyMode    `json:"proxy"`
	CustomScript        string       `json:"custom_script"`
	CustomPlaceholders  Placeholders `json:"custom_placeholders"`
}

// Validation reasons reported in ValidationError.Reason.
const (
	ReasonRequired    = "required"
	ReasonInvalid     = "invalid"
	ReasonScheme      = "scheme"
	ReasonNotNumeric  = "not_numeric"
	ReasonNegative    = "negative"
	ReasonUnsupported = "unsupported"
)

// ValidationError names the first field that failed validation.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string { return fmt.Sprintf("%s: %s", e.Field, e.Reason) }

// ErrNotFound is returned when an account id does not exist.
var ErrNotFound = errors.New("account not found")

// apply validates in against the stored account (nil for create) and returns
// the resulting record without an ID or order.
func apply(in Input, prev *Account) (Account, error) {
	out := Account{
		Name:                strings.TrimSpace(in.Name),
		Source:              in.Source,
		UserID:              strings.TrimSpace(in.UserID),
		LowBalanceThreshold: in.LowBalanceThreshold,
		Proxy:               in.Proxy,
		CustomScript:        in.CustomScript,
		CustomPlaceholders: Placeholders{
			BaseURL:     strings.TrimSpace(in.CustomPlaceholders.BaseURL),
			APIKey:      strings.TrimSpace(in.CustomPlaceholders.APIKey),
			AccessToken: strings.TrimSpace(in.CustomPlaceholders.AccessToken),
			UserID:      strings.TrimSpace(in.CustomPlaceholders.UserID),
		},
	}
	if out.Name == "" {
		return Account{}, &ValidationError{"name", ReasonRequired}
	}
	if err := resolveQuery(&out, in, prev); err != nil {
		return Account{}, err
	}
	if t := out.LowBalanceThreshold; t != nil {
		if math.IsNaN(*t) || math.IsInf(*t, 0) {
			return Account{}, &ValidationError{"low_balance_threshold", ReasonInvalid}
		}
		if *t < 0 {
			return Account{}, &ValidationError{"low_balance_threshold", ReasonNegative}
		}
		v := *t
		out.LowBalanceThreshold = &v
	}
	if err := resolveProxy(&out); err != nil {
		return Account{}, err
	}
	return out, nil
}

// Resolve validates the fields that decide how an account is queried
// (source, site address, credential, user ID, proxy) and merges them with
// the stored account prev (nil for an unsaved one): a blank or masked
// credential means the stored credential. It never touches the data file
// and does not check the name or the low-balance threshold, so a form can be
// tried before it is complete.
func Resolve(in Input, prev *Account) (Account, error) {
	out := Account{
		Source:       in.Source,
		UserID:       strings.TrimSpace(in.UserID),
		Proxy:        in.Proxy,
		CustomScript: in.CustomScript,
		CustomPlaceholders: Placeholders{
			BaseURL:     strings.TrimSpace(in.CustomPlaceholders.BaseURL),
			APIKey:      strings.TrimSpace(in.CustomPlaceholders.APIKey),
			AccessToken: strings.TrimSpace(in.CustomPlaceholders.AccessToken),
			UserID:      strings.TrimSpace(in.CustomPlaceholders.UserID),
		},
	}
	if err := resolveQuery(&out, in, prev); err != nil {
		return Account{}, err
	}
	if err := resolveProxy(&out); err != nil {
		return Account{}, err
	}
	return out, nil
}

// resolveQuery dispatches on the data source and checks the fields that
// source queries with, taking blank secrets from prev. apply and Resolve
// share it so a saved account and a tried one follow the same rules.
func resolveQuery(out *Account, in Input, prev *Account) error {
	if out.Source == "" {
		out.Source = SourceNewAPI
	}
	switch out.Source {
	case SourceNewAPI:
	case SourceCustom:
		return resolveCustom(out, prev)
	default:
		return &ValidationError{"source", ReasonInvalid}
	}
	base, err := NormalizeBaseURL(in.BaseURL)
	if err != nil {
		return err
	}
	out.BaseURL = base

	out.Credential = strings.TrimSpace(in.Credential)
	if prev != nil {
		if out.Credential == "" || out.Credential == Mask(prev.Credential) {
			out.Credential = prev.Credential
		}
		keepSecret(&out.CustomPlaceholders.APIKey, prev.CustomPlaceholders.APIKey)
		keepSecret(&out.CustomPlaceholders.AccessToken, prev.CustomPlaceholders.AccessToken)
	}
	if out.Credential == "" {
		return &ValidationError{"credential", ReasonRequired}
	}
	if !out.IsPAT() && out.UserID == "" {
		return &ValidationError{"user_id", ReasonRequired}
	}
	if out.UserID != "" && !isDigits(out.UserID) {
		return &ValidationError{"user_id", ReasonNotNumeric}
	}
	return nil
}

// resolveCustom checks a custom-query account. The script must be present;
// baseUrl is optional but, when given, is a site address like a built-in
// one and also becomes the account's base_url (the host shown in lists).
// The built-in credential and user ID do not apply and are cleared.
func resolveCustom(out *Account, prev *Account) error {
	if strings.TrimSpace(out.CustomScript) == "" {
		return &ValidationError{"custom_script", ReasonRequired}
	}
	ph := &out.CustomPlaceholders
	if ph.BaseURL != "" {
		base, err := NormalizeBaseURL(ph.BaseURL)
		if err != nil {
			return &ValidationError{"custom_placeholders.baseUrl", err.(*ValidationError).Reason}
		}
		ph.BaseURL = base
	}
	if prev != nil {
		keepSecret(&ph.APIKey, prev.CustomPlaceholders.APIKey)
		keepSecret(&ph.AccessToken, prev.CustomPlaceholders.AccessToken)
	}
	out.BaseURL, out.Credential, out.UserID = ph.BaseURL, "", ""
	return nil
}

func resolveProxy(out *Account) error {
	switch out.Proxy {
	case "":
		out.Proxy = ProxyFollow
	case ProxyFollow, ProxyDirect:
	default:
		return &ValidationError{"proxy", ReasonInvalid}
	}
	return nil
}

func keepSecret(dst *string, prev string) {
	if *dst == "" || *dst == Mask(prev) {
		*dst = prev
	}
}

// NormalizeBaseURL accepts an absolute http(s) site address without
// credentials, query or fragment and strips trailing slashes.
func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", &ValidationError{"base_url", ReasonRequired}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		if err == nil && u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https" {
			return "", &ValidationError{"base_url", ReasonScheme}
		}
		return "", &ValidationError{"base_url", ReasonInvalid}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", &ValidationError{"base_url", ReasonScheme}
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host+u.EscapedPath(), "/"), nil
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// Mask returns a display form of a secret that never reveals the whole value:
// secrets shorter than 16 bytes are fully hidden, longer ones keep only their
// first and last four bytes (at most half of the value).
func Mask(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) < 16 {
		return "••••••••"
	}
	return secret[:4] + "••••" + secret[len(secret)-4:]
}
