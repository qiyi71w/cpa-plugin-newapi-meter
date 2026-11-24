package newapi

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/outbound"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/status"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/store"
)

// fakeSite is a synthetic new-api upstream. Each endpoint answers with the
// handler registered for it, or a success envelope built from data.
type fakeSite struct {
	*httptest.Server
	mu       sync.Mutex
	status   map[string]any
	handlers map[string]http.HandlerFunc
	requests map[string]*http.Request
}

func newFakeSite(t *testing.T) *fakeSite {
	f := &fakeSite{
		status: map[string]any{
			"quota_per_unit":     500000.0,
			"quota_display_type": "USD",
			"version":            "v0.0.0-test",
			"enable_data_export": true,
		},
		handlers: map[string]http.HandlerFunc{},
		requests: map[string]*http.Request{},
	}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests[r.URL.Path] = r.Clone(context.Background())
		h := f.handlers[r.URL.Path]
		st := f.status
		f.mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}
		switch r.URL.Path {
		case EndpointStatus:
			ok(w, st)
		case EndpointSelf:
			ok(w, map[string]any{"quota": 5000000, "used_quota": 2500000})
		case EndpointStat:
			ok(w, map[string]any{"quota": 250000, "rpm": 3, "tpm": 4200})
		case EndpointData:
			ok(w, []map[string]any{{"model_name": "model-a", "created_at": 1, "count": 2, "token_used": 30, "quota": 40}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func ok(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "", "data": data})
}

func (f *fakeSite) on(path string, h http.HandlerFunc) {
	f.mu.Lock()
	f.handlers[path] = h
	f.mu.Unlock()
}

func (f *fakeSite) setStatus(m map[string]any) {
	f.mu.Lock()
	f.status = m
	f.mu.Unlock()
}

func (f *fakeSite) request(path string) *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[path]
}

func respond(code int, contentType, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}
}

var unauthorized = respond(401, "application/json", `{"success":false,"message":"unauthorized"}`)

const (
	testPAT    = "nap_synthetic_personal_access_token_0001"
	testLegacy = "synthetic-legacy-access-token-0002"
)

func account(f *fakeSite, cred string) store.Account {
	a := store.Account{ID: "acc_test", Name: "Synthetic", Source: store.SourceNewAPI, BaseURL: f.URL, Credential: cred, Proxy: store.ProxyDirect}
	if cred == testLegacy {
		a.UserID = "42"
	}
	return a
}

var fixedNow = time.Date(2026, 3, 15, 10, 30, 0, 0, time.FixedZone("UTC+8", 8*3600))

func source(now time.Time) *Source {
	return &Source{HTTP: outbound.New(nil), Now: func() time.Time { return now }}
}

func fetch(t *testing.T, f *fakeSite, cred string, threshold *float64) (Reading, Summary) {
	t.Helper()
	r := source(fixedNow).Fetch(context.Background(), account(f, cred))
	return r, Summarize(r, threshold)
}

func near(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 1e-9 {
		t.Fatalf("%s = %v, want %v", name, deref(got), want)
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func ptr(v float64) *float64 { return &v }

func TestDisplayCurrencies(t *testing.T) {
	cases := []struct {
		name     string
		status   map[string]any
		currency Currency
		balance  float64
		today    float64
	}{
		{"USD", map[string]any{"quota_per_unit": 500000, "quota_display_type": "USD"}, Currency{"USD", "$"}, 10, 0.5},
		{"CNY", map[string]any{"quota_per_unit": 500000, "quota_display_type": "CNY", "usd_exchange_rate": 7}, Currency{"CNY", "¥"}, 70, 3.5},
		{"CUSTOM", map[string]any{"quota_per_unit": 500000, "quota_display_type": "CUSTOM", "custom_currency_symbol": "€", "custom_currency_exchange_rate": 0.5}, Currency{"CUSTOM", "€"}, 5, 0.25},
		{"TOKENS", map[string]any{"quota_per_unit": 500000, "quota_display_type": "TOKENS"}, Currency{"TOKENS", ""}, 5000000, 250000},
		{"missing type, display_in_currency=false", map[string]any{"quota_per_unit": 500000, "display_in_currency": false}, Currency{"TOKENS", ""}, 5000000, 250000},
		{"missing type, display_in_currency=true", map[string]any{"quota_per_unit": 500000, "display_in_currency": true}, Currency{"USD", "$"}, 10, 0.5},
		{"missing type and flag", map[string]any{"quota_per_unit": 500000}, Currency{"USD", "$"}, 10, 0.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSite(t)
			f.setStatus(tc.status)
			_, s := fetch(t, f, testPAT, nil)
			if s.Currency == nil || *s.Currency != tc.currency {
				t.Fatalf("currency = %+v, want %+v", s.Currency, tc.currency)
			}
			near(t, "balance", s.Balance, tc.balance)
			near(t, "today", s.TodaySpend, tc.today)
			want := tc.balance / 2 // used_quota is half the fake quota
			near(t, "total used", s.TotalUsed, want)
			if s.State != status.OK {
				t.Fatalf("state = %s", s.State)
			}
		})
	}
}

func TestCustomCurrencyWithoutRateIsBadResponse(t *testing.T) {
	f := newFakeSite(t)
	f.setStatus(map[string]any{"quota_per_unit": 500000, "quota_display_type": "CNY"})
	_, s := fetch(t, f, testPAT, nil)
	if s.State != status.Partial || s.Balance != nil || s.BalanceFailure == nil || s.BalanceFailure.Code != status.BadResponse {
		t.Fatalf("summary = %+v", s)
	}
}

func TestPATSendsOnlyBearer(t *testing.T) {
	f := newFakeSite(t)
	fetch(t, f, testPAT, nil)
	for _, path := range []string{EndpointSelf, EndpointStat, EndpointData} {
		r := f.request(path)
		if got := r.Header.Get("Authorization"); got != "Bearer "+testPAT {
			t.Fatalf("%s Authorization = %q", path, got)
		}
		if _, present := r.Header["New-Api-User"]; present {
			t.Fatalf("%s sent New-Api-User with a PAT", path)
		}
	}
	if r := f.request(EndpointStatus); r.Header.Get("Authorization") != "" {
		t.Fatal("public /api/status received the credential")
	}
}

func TestLegacyTokenSendsUserID(t *testing.T) {
	f := newFakeSite(t)
	r, _ := fetch(t, f, testLegacy, nil)
	if r.CredentialKind != KindLegacy {
		t.Fatalf("kind = %s", r.CredentialKind)
	}
	for _, path := range []string{EndpointSelf, EndpointStat, EndpointData} {
		req := f.request(path)
		if req.Header.Get("Authorization") != "Bearer "+testLegacy || req.Header.Get("New-Api-User") != "42" {
			t.Fatalf("%s headers = %v", path, req.Header)
		}
	}
}

func TestTodayStartsAtLocalMidnight(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		now   time.Time
		start time.Time
	}{
		{"UTC+8", fixedNow, time.Date(2026, 3, 15, 0, 0, 0, 0, fixedNow.Location())},
		// 2026-03-08 is the US spring-forward day: midnight is still 05:00 UTC.
		{"DST start", time.Date(2026, 3, 8, 15, 0, 0, 0, ny), time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC)},
		// 2026-11-01 is the fall-back day: midnight is 04:00 UTC (still EDT).
		{"DST end", time.Date(2026, 11, 1, 23, 0, 0, 0, ny), time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSite(t)
			r := source(tc.now).Fetch(context.Background(), account(f, testPAT))
			if !r.TodayStart.Equal(tc.start) {
				t.Fatalf("today start = %s, want %s", r.TodayStart, tc.start)
			}
			for _, path := range []string{EndpointStat, EndpointData} {
				q := f.request(path).URL.Query()
				if q.Get("start_timestamp") != strconv.FormatInt(tc.start.Unix(), 10) || q.Get("end_timestamp") != strconv.FormatInt(tc.now.Unix(), 10) {
					t.Fatalf("%s query = %s", path, q.Encode())
				}
			}
			if q := f.request(EndpointStat).URL.Query(); q.Get("type") != "2" {
				t.Fatalf("stat type = %q", q.Get("type"))
			}
		})
	}
}

func TestAccountStatus(t *testing.T) {
	html := respond(200, "text/html; charset=utf-8", "<!doctype html><title>verify</title>")
	cases := []struct {
		name      string
		setup     func(f *fakeSite)
		threshold *float64
		want      status.State
		check     func(t *testing.T, s Summary)
	}{
		{"normal", func(*fakeSite) {}, ptr(5), status.OK, nil},
		{"low balance", func(*fakeSite) {}, ptr(20), status.Low, nil},
		{"user/self HTML, rest fine → partial", func(f *fakeSite) { f.on(EndpointSelf, html) }, ptr(5), status.Partial,
			func(t *testing.T, s Summary) {
				if s.Balance != nil || s.BalanceFailure == nil || s.BalanceFailure.Code != status.SiteBlocked {
					t.Fatalf("balance failure = %+v", s.BalanceFailure)
				}
				near(t, "today", s.TodaySpend, 0.5)
			}},
		{"all credentialed 401 → needs action", func(f *fakeSite) {
			f.on(EndpointSelf, unauthorized)
			f.on(EndpointStat, unauthorized)
			f.on(EndpointData, unauthorized)
		}, ptr(5), status.NeedsAction,
			func(t *testing.T, s Summary) {
				if s.Primary == nil || s.Primary.Code != status.CredentialInvalid {
					t.Fatalf("primary = %+v", s.Primary)
				}
			}},
		{"only data/self fails → partial", func(f *fakeSite) { f.on(EndpointData, respond(502, "text/plain", "bad gateway")) }, ptr(5), status.Partial,
			func(t *testing.T, s Summary) {
				near(t, "balance", s.Balance, 10)
				if len(s.Failures) != 1 || s.Failures[0].Name != EndpointData || s.Failures[0].Failure.Code != status.SiteError {
					t.Fatalf("failures = %+v", s.Failures)
				}
			}},
		{"data export off: data/self failure ignored", func(f *fakeSite) {
			f.setStatus(map[string]any{"quota_per_unit": 500000, "quota_display_type": "USD", "enable_data_export": false})
			f.on(EndpointData, respond(500, "application/json", `{"success":false}`))
		}, ptr(5), status.OK,
			func(t *testing.T, s Summary) {
				if s.DataExport == nil || *s.DataExport || len(s.Failures) != 0 {
					t.Fatalf("data export = %v failures = %+v", s.DataExport, s.Failures)
				}
			}},
		{"data export off: other two 401 → needs action", func(f *fakeSite) {
			f.setStatus(map[string]any{"quota_per_unit": 500000, "quota_display_type": "USD", "enable_data_export": false})
			f.on(EndpointSelf, unauthorized)
			f.on(EndpointStat, unauthorized)
		}, nil, status.NeedsAction, nil},
		{"status fails, credentialed fine → partial", func(f *fakeSite) { f.on(EndpointStatus, respond(503, "text/plain", "down")) }, ptr(5), status.Partial,
			func(t *testing.T, s Summary) {
				if s.Balance != nil || s.TodaySpend != nil || s.BalanceFailure == nil || s.BalanceFailure.Code != status.SiteError {
					t.Fatalf("summary = %+v", s)
				}
				if s.RPM == nil || *s.RPM != 3 {
					t.Fatalf("rpm = %v", deref(s.RPM))
				}
			}},
		{"partial below threshold is not low", func(f *fakeSite) { f.on(EndpointData, unauthorized) }, ptr(1000), status.Partial, nil},
		{"no threshold is never low", func(*fakeSite) {}, nil, status.OK, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSite(t)
			tc.setup(f)
			_, s := fetch(t, f, testPAT, tc.threshold)
			if s.State != tc.want {
				t.Fatalf("state = %s, want %s (failures %+v)", s.State, tc.want, s.Failures)
			}
			if tc.check != nil {
				tc.check(t, s)
			}
		})
	}
}

func TestRequestTimeoutIsUnreachable(t *testing.T) {
	f := newFakeSite(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	f.on(EndpointSelf, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	src := source(fixedNow)
	src.Timeout = 50 * time.Millisecond
	r := src.Fetch(context.Background(), account(f, testPAT))
	got := r.Probes[1]
	if got.Name != EndpointSelf || got.Failure == nil || got.Failure.Code != status.SiteUnreachable || got.Failure.Diagnostic != "NETWORK_TIMEOUT" {
		t.Fatalf("probe = %+v %+v", got, got.Failure)
	}
}
