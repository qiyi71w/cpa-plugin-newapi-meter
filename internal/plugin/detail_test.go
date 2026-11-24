package plugin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// periodSite is a synthetic new-api site with usage rows that, like a site
// whose log_setting.max_range_days is low, refuses longer ranges with
// success:false.
type periodSite struct {
	*httptest.Server
	hits       sync.Map // path → *atomic.Int32
	maxDays    atomic.Int64
	dataExport atomic.Bool
}

func newPeriodSite(t *testing.T) *periodSite {
	s := &periodSite{}
	s.maxDays.Store(365)
	s.dataExport.Store(true)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := s.hits.LoadOrStore(r.URL.Path, new(atomic.Int32))
		n.(*atomic.Int32).Add(1)
		w.Header().Set("Content-Type", "application/json")
		from, _ := strconv.ParseInt(r.URL.Query().Get("start_timestamp"), 10, 64)
		to, _ := strconv.ParseInt(r.URL.Query().Get("end_timestamp"), 10, 64)
		if (r.URL.Path == "/api/log/self/stat" || r.URL.Path == "/api/data/self") && to-from > s.maxDays.Load()*86400 {
			_, _ = w.Write([]byte(`{"success":false,"message":"time range too long"}`))
			return
		}
		var data any
		switch r.URL.Path {
		case "/api/status":
			data = map[string]any{"quota_per_unit": 500000, "quota_display_type": "USD", "version": "v0.0.0-test", "enable_data_export": s.dataExport.Load()}
		case "/api/user/self":
			data = map[string]any{"quota": 5000000, "used_quota": 1000000}
		case "/api/log/self/stat":
			data = map[string]any{"quota": 1500000, "rpm": 2, "tpm": 900}
		case "/api/data/self":
			if !s.dataExport.Load() {
				w.WriteHeader(500)
				_, _ = w.Write([]byte(`{"success":false}`))
				return
			}
			hour := from - from%3600 + 3600
			data = []map[string]any{
				{"model_name": "model-a", "created_at": hour, "count": 3, "token_used": 300, "quota": 1000000},
				{"model_name": "model-b", "created_at": hour, "count": 1, "token_used": 100, "quota": 500000, "use_group": "default"},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *periodSite) count(path string) int32 {
	n, ok := s.hits.Load(path)
	if !ok {
		return 0
	}
	return n.(*atomic.Int32).Load()
}

func (h *harness) createAt(base, name string) string {
	h.t.Helper()
	r := h.do("POST", RoutePrefix+"/accounts", nil, map[string]any{"name": name, "base_url": base, "credential": secretPAT})
	if r.Status != http.StatusCreated {
		h.t.Fatalf("create: %d %s", r.Status, r.Body)
	}
	return r.json(h.t)["account"].(map[string]any)["id"].(string)
}

func (h *harness) detail(q url.Values) map[string]any {
	h.t.Helper()
	r := h.do("GET", RoutePrefix+"/detail", q, nil)
	if r.Status != 200 {
		h.t.Fatalf("detail: %d %s", r.Status, r.Body)
	}
	assertNoSecrets(h.t, "detail response", string(r.Body))
	return r.json(h.t)
}

func TestDetailTodayReusesTheOverviewQuery(t *testing.T) {
	h := newHarness(t)
	s := newPeriodSite(t)
	id := h.createAt(s.URL, "A")
	ov := h.overview(nil)[0]

	for _, q := range []url.Values{
		{"id": {id}},
		{"id": {id}, "period": {"today"}},
		{"id": {id}, "period": {"custom"}, "start": {"2026-05-02"}, "end": {"2026-05-02"}},
	} {
		d := h.detail(q)
		if s.count("/api/data/self") != 1 || s.count("/api/log/self/stat") != 1 || s.count("/api/status") != 1 {
			t.Fatalf("today's detail queried the site again: data %d stat %d", s.count("/api/data/self"), s.count("/api/log/self/stat"))
		}
		if d["cached"] != true || d["spend"] != ov["today_spend"] || d["spend"] != float64(3) {
			t.Fatalf("detail = %v", d)
		}
		per := d["period"].(map[string]any)
		// The harness clock is 2026-05-02 09:00 UTC+8.
		if per["granularity"] != "hour" || per["includes_now"] != true || per["minutes"] != float64(540) || per["start"] != "2026-05-02T00:00:00+08:00" || d["timezone"] != "UTC+8" {
			t.Fatalf("period = %v", per)
		}
		u := d["usage"].(map[string]any)
		models := u["models"].([]any)
		if u["requests"] != float64(4) || u["spend"] != float64(3) || len(models) != 2 || models[0].(map[string]any)["model"] != "model-a" ||
			len(u["buckets"].([]any)) != 9 || u["avg_rpm"] != 4.0/540 {
			t.Fatalf("usage = %v", u)
		}
	}
}

// detailWithin is detail with a deadline, so a period whose bucket loop stops
// advancing fails instead of hanging the test run.
func (h *harness) detailWithin(q url.Values) map[string]any {
	h.t.Helper()
	done := make(chan reply, 1)
	go func() { done <- h.do("GET", RoutePrefix+"/detail", q, nil) }()
	select {
	case r := <-done:
		if r.Status != 200 {
			h.t.Fatalf("detail %v: %d %s", q, r.Status, r.Body)
		}
		return r.json(h.t)
	case <-time.After(10 * time.Second):
		h.t.Fatalf("detail %v did not return", q)
		return nil
	}
}

func TestDetailWhereDSTSkipsMidnight(t *testing.T) {
	// America/Santiago has no 00:00 on 2026-09-06: the day starts at 01:00 -03.
	scl, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t)
	at := func(d, hour int) {
		h.clock.mu.Lock()
		h.clock.t = time.Date(2026, 9, d, hour, 0, 0, 0, scl)
		h.clock.mu.Unlock()
	}
	at(6, 12)
	s := newPeriodSite(t)
	id := h.createAt(s.URL, "A")

	ov := h.do("GET", RoutePrefix+"/overview", nil, nil)
	if got := ov.json(t)["today_start"]; got != "2026-09-06T01:00:00-03:00" {
		t.Fatalf("overview today_start = %v", got)
	}
	today := h.detailWithin(url.Values{"id": {id}, "period": {"today"}})["period"].(map[string]any)
	if today["start_date"] != "2026-09-06" || today["start"] != "2026-09-06T01:00:00-03:00" || today["minutes"] != float64(11*60) {
		t.Fatalf("today = %v", today)
	}

	at(10, 12)
	d := h.detailWithin(url.Values{"id": {id}, "period": {"custom"}, "start": {"2026-09-04"}, "end": {"2026-09-08"}})
	if n := len(d["usage"].(map[string]any)["buckets"].([]any)); n != 5 {
		t.Fatalf("custom 2026-09-04..08 has %d day buckets", n)
	}

	at(8, 12)
	d = h.detailWithin(url.Values{"id": {id}, "period": {"7d"}})
	if n := len(d["usage"].(map[string]any)["buckets"].([]any)); n != 7 {
		t.Fatalf("7d has %d day buckets", n)
	}
	at(7, 12)
	y := h.detailWithin(url.Values{"id": {id}, "period": {"yesterday"}})["period"].(map[string]any)
	if y["start"] != "2026-09-06T01:00:00-03:00" || y["minutes"] != float64(23*60) {
		t.Fatalf("yesterday = %v", y)
	}
}

func TestDetailPeriodCacheAndForce(t *testing.T) {
	h := newHarness(t)
	s := newPeriodSite(t)
	id := h.createAt(s.URL, "A")
	week := url.Values{"id": {id}, "period": {"7d"}}

	d := h.detail(week)
	per := d["period"].(map[string]any)
	if d["cached"] != false || per["start_date"] != "2026-04-26" || per["end_date"] != "2026-05-02" || per["granularity"] != "day" || len(d["usage"].(map[string]any)["buckets"].([]any)) != 7 {
		t.Fatalf("7d = %v", d)
	}
	h.clock.add(30 * time.Second)
	if h.detail(week)["cached"] != true || s.count("/api/data/self") != 1 {
		t.Fatal("within 60 s the period was queried again")
	}
	h.detail(url.Values{"id": {id}, "period": {"30d"}})
	if s.count("/api/data/self") != 2 {
		t.Fatal("another period was served from the 7d entry")
	}
	h.detail(url.Values{"id": {id}, "period": {"7d"}, "force": {"1"}})
	if s.count("/api/data/self") != 3 {
		t.Fatal("force did not bypass the cache")
	}
	h.clock.add(61 * time.Second)
	h.detail(week)
	if s.count("/api/data/self") != 4 {
		t.Fatal("expired entry was served")
	}
	if s.count("/api/user/self") != 0 {
		t.Fatal("a period query read the balance")
	}
	// Editing the account drops its cached periods.
	if r := h.do("PUT", RoutePrefix+"/accounts", nil, map[string]any{"id": id, "name": "B", "base_url": s.URL}); r.Status != 200 {
		t.Fatalf("update: %d %s", r.Status, r.Body)
	}
	if h.detail(week)["cached"] != false {
		t.Fatal("period cache survived an account update")
	}
}

func TestDetailPeriodFailureLeavesStatusAlone(t *testing.T) {
	h := newHarness(t)
	s := newPeriodSite(t)
	s.maxDays.Store(7)
	id := h.createAt(s.URL, "A")

	d := h.detail(url.Values{"id": {id}, "period": {"30d"}})
	for _, part := range []string{"spend_failure", "usage_failure"} {
		f, _ := d[part].(map[string]any)
		if f == nil || f["code"] != "bad_response" || f["diagnostic"] != "SUCCESS_FALSE" {
			t.Fatalf("%s = %v", part, d[part])
		}
	}
	if d["spend"] != nil || d["usage"] != nil || d["usage_disabled"] != false {
		t.Fatalf("detail = %v", d)
	}
	if ov := h.overview(nil)[0]; ov["state"] != "ok" || len(ov["failures"].([]any)) != 0 {
		t.Fatalf("a failed period changed the account: %v", ov)
	}
	if h.detail(url.Values{"id": {id}, "period": {"7d"}})["usage"] == nil {
		t.Fatal("a period within the site's limit failed")
	}
	assertNoSecrets(t, "plugin log", h.logs.String())
}

func TestDetailDashboardDisabled(t *testing.T) {
	h := newHarness(t)
	s := newPeriodSite(t)
	s.dataExport.Store(false)
	id := h.createAt(s.URL, "A")
	for _, period := range []string{"today", "yesterday"} {
		d := h.detail(url.Values{"id": {id}, "period": {period}})
		if d["usage_disabled"] != true || d["usage_failure"] != nil || d["usage"] != nil || d["data_export"] != false || d["spend"] != float64(3) {
			t.Fatalf("%s = %v", period, d)
		}
	}
	if ov := h.overview(nil)[0]; ov["state"] != "ok" {
		t.Fatalf("dashboard off changed the state: %v", ov["state"])
	}
}

func TestDetailRejectsInvalidRequests(t *testing.T) {
	h := newHarness(t)
	s := newPeriodSite(t)
	id := h.createAt(s.URL, "A")
	cases := []struct {
		name          string
		q             url.Values
		status        int
		code          string
		field, reason string
	}{
		{"missing id", url.Values{}, 400, "validation_failed", "id", "required"},
		{"unknown account", url.Values{"id": {"acc_0000000000000000"}}, 404, "account_not_found", "", ""},
		{"unknown preset", url.Values{"id": {id}, "period": {"90d"}}, 400, "validation_failed", "period", "unsupported"},
		{"swapped dates", url.Values{"id": {id}, "period": {"custom"}, "start": {"2026-05-01"}, "end": {"2026-04-01"}}, 400, "validation_failed", "end", "swapped"},
		{"over 365 days", url.Values{"id": {id}, "period": {"custom"}, "start": {"2025-05-02"}, "end": {"2026-05-02"}}, 400, "validation_failed", "end", "too_long"},
		{"after today", url.Values{"id": {id}, "period": {"custom"}, "start": {"2026-05-01"}, "end": {"2026-05-03"}}, 400, "validation_failed", "end", "future"},
		{"bad date", url.Values{"id": {id}, "period": {"custom"}, "start": {"May 1"}, "end": {"2026-05-02"}}, 400, "validation_failed", "start", "invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := h.do("GET", RoutePrefix+"/detail", tc.q, nil)
			e, _ := r.json(t)["error"].(map[string]any)
			if r.Status != tc.status || e == nil || e["code"] != tc.code || (tc.field != "" && (e["field"] != tc.field || e["reason"] != tc.reason)) {
				t.Fatalf("response = %d %s", r.Status, r.Body)
			}
		})
	}
	if s.count("/api/status") != 0 {
		t.Fatal("an invalid request reached the site")
	}
}
