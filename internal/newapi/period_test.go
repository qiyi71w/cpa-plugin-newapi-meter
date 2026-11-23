package newapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/status"
)

func newYork(t *testing.T) *time.Location {
	t.Helper()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	return ny
}

func resolve(t *testing.T, now time.Time, preset, start, end string) Period {
	t.Helper()
	p, err := ResolvePeriod(now, preset, start, end)
	if err != nil {
		t.Fatalf("ResolvePeriod(%s, %q, %q): %v", preset, start, end, err)
	}
	return p
}

func TestPresetPeriodsAcrossDST(t *testing.T) {
	ny := newYork(t)
	// 2026-03-08 is the US spring-forward day (23 hours); 2026-11-01 falls back (25 hours).
	afterSpring := time.Date(2026, 3, 9, 10, 0, 0, 0, ny)
	afterFall := time.Date(2026, 11, 2, 8, 0, 0, 0, ny)
	cases := []struct {
		name        string
		now         time.Time
		preset      string
		start, end  time.Time
		first, last string
		days        int
		granularity string
		buckets     int // as of now
	}{
		{"today", afterSpring, PresetToday, time.Date(2026, 3, 9, 4, 0, 0, 0, time.UTC), time.Date(2026, 3, 10, 4, 0, 0, 0, time.UTC),
			"2026-03-09", "2026-03-09", 1, GranularityHour, 10},
		{"default is today", afterSpring, "", time.Date(2026, 3, 9, 4, 0, 0, 0, time.UTC), time.Date(2026, 3, 10, 4, 0, 0, 0, time.UTC),
			"2026-03-09", "2026-03-09", 1, GranularityHour, 10},
		{"yesterday, 23-hour day", afterSpring, PresetYesterday, time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC), time.Date(2026, 3, 9, 4, 0, 0, 0, time.UTC),
			"2026-03-08", "2026-03-08", 1, GranularityHour, 23},
		{"yesterday, 25-hour day", afterFall, PresetYesterday, time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC), time.Date(2026, 11, 2, 5, 0, 0, 0, time.UTC),
			"2026-11-01", "2026-11-01", 1, GranularityHour, 25},
		{"7d across spring forward", afterSpring, Preset7d, time.Date(2026, 3, 3, 5, 0, 0, 0, time.UTC), time.Date(2026, 3, 10, 4, 0, 0, 0, time.UTC),
			"2026-03-03", "2026-03-09", 7, GranularityDay, 7},
		{"30d across fall back", afterFall, Preset30d, time.Date(2026, 10, 4, 4, 0, 0, 0, time.UTC), time.Date(2026, 11, 3, 5, 0, 0, 0, time.UTC),
			"2026-10-04", "2026-11-02", 30, GranularityDay, 30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := resolve(t, tc.now, tc.preset, "", "")
			if !p.Start.Equal(tc.start) || !p.End.Equal(tc.end) {
				t.Fatalf("range = %s – %s, want %s – %s", p.Start.UTC(), p.End.UTC(), tc.start, tc.end)
			}
			if p.StartDate != tc.first || p.EndDate != tc.last || p.Days != tc.days || p.Granularity != tc.granularity {
				t.Fatalf("period = %+v", p)
			}
			starts := p.bucketStarts(tc.now)
			if len(starts) != tc.buckets {
				t.Fatalf("buckets = %d, want %d", len(starts), tc.buckets)
			}
			if p.Granularity == GranularityDay {
				for _, s := range starts {
					if s.Hour() != 0 || s.Minute() != 0 {
						t.Fatalf("day bucket starts at %s, not local midnight", s)
					}
				}
			}
		})
	}
}

func TestCustomPeriods(t *testing.T) {
	ny := newYork(t)
	now := time.Date(2026, 3, 9, 10, 0, 0, 0, ny)

	// Inclusive dates across spring forward: 3 local days are 71 hours.
	p := resolve(t, now, PresetCustom, "2026-03-07", "2026-03-09")
	if !p.Start.Equal(time.Date(2026, 3, 7, 5, 0, 0, 0, time.UTC)) || !p.End.Equal(time.Date(2026, 3, 10, 4, 0, 0, 0, time.UTC)) || p.Days != 3 {
		t.Fatalf("custom = %+v", p)
	}
	if !p.IncludesNow(now) || p.IsToday(now) {
		t.Fatalf("includes now = %v, is today = %v", p.IncludesNow(now), p.IsToday(now))
	}
	if today := resolve(t, now, PresetCustom, "2026-03-09", "2026-03-09"); !today.IsToday(now) {
		t.Fatal("a custom period of just today does not reuse today's query")
	}

	// The 2-day boundary: two days are hourly, three are daily.
	two := resolve(t, now, PresetCustom, "2026-03-06", "2026-03-07")
	if two.Granularity != GranularityHour || len(two.bucketStarts(now)) != 48 {
		t.Fatalf("2 days = %s with %d buckets", two.Granularity, len(two.bucketStarts(now)))
	}
	three := resolve(t, now, PresetCustom, "2026-03-05", "2026-03-07")
	if three.Granularity != GranularityDay || len(three.bucketStarts(now)) != 3 {
		t.Fatalf("3 days = %s with %d buckets", three.Granularity, len(three.bucketStarts(now)))
	}
	if long := resolve(t, now, PresetCustom, "2025-03-10", "2026-03-09"); long.Days != MaxPeriodDays {
		t.Fatalf("365-day period = %d days", long.Days)
	}

	invalid := []struct {
		name, preset, start, end string
		field, reason            string
	}{
		{"swapped", PresetCustom, "2026-03-05", "2026-03-04", "end", ReasonSwapped},
		{"366 days", PresetCustom, "2025-03-09", "2026-03-09", "end", ReasonTooLong},
		{"end after today", PresetCustom, "2026-03-01", "2026-03-10", "end", ReasonFuture},
		{"start after today", PresetCustom, "2026-03-10", "2026-03-11", "start", ReasonFuture},
		{"missing start", PresetCustom, "", "2026-03-09", "start", "required"},
		{"missing end", PresetCustom, "2026-03-01", "", "end", "required"},
		{"bad date", PresetCustom, "2026-3-1", "2026-03-09", "start", "invalid"},
		{"no such day", PresetCustom, "2026-02-30", "2026-03-09", "start", "invalid"},
		{"unknown preset", "90d", "", "", "period", "unsupported"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolvePeriod(now, tc.preset, tc.start, tc.end)
			var pe *PeriodError
			if !errors.As(err, &pe) || pe.Field != tc.field || pe.Reason != tc.reason {
				t.Fatalf("err = %v, want %s/%s", err, tc.field, tc.reason)
			}
		})
	}
}

func TestPeriodQueryTimestamps(t *testing.T) {
	cases := []struct {
		name     string
		preset   string
		from, to int64
	}{
		// fixedNow is 2026-03-15 10:30 UTC+8.
		{"running period ends now", Preset7d, time.Date(2026, 3, 9, 0, 0, 0, 0, fixedNow.Location()).Unix(), fixedNow.Unix()},
		{"past period ends a second before its midnight", PresetYesterday,
			time.Date(2026, 3, 14, 0, 0, 0, 0, fixedNow.Location()).Unix(), time.Date(2026, 3, 15, 0, 0, 0, 0, fixedNow.Location()).Unix() - 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSite(t)
			p := resolve(t, fixedNow, tc.preset, "", "")
			source(fixedNow).FetchPeriod(context.Background(), account(f, testPAT), p)
			for _, path := range []string{EndpointStat, EndpointData} {
				q := f.request(path).URL.Query()
				if q.Get("start_timestamp") != strconv.FormatInt(tc.from, 10) || q.Get("end_timestamp") != strconv.FormatInt(tc.to, 10) {
					t.Fatalf("%s query = %s", path, q.Encode())
				}
			}
			if f.request(EndpointStat).URL.Query().Get("type") != "2" {
				t.Fatal("stat query is not type=2")
			}
			if f.request(EndpointSelf) != nil {
				t.Fatal("a period query read the balance")
			}
		})
	}
}

func usd() *Site { return &Site{Currency: Currency{"USD", "$"}, quotaPerUnit: 500000, rate: 1} }

func TestAggregateByModel(t *testing.T) {
	p := resolve(t, fixedNow, PresetToday, "", "")
	h := func(hour int) int64 { return p.Start.Add(time.Duration(hour) * time.Hour).Unix() }
	rows := []UsageRow{
		{Model: "model-a", CreatedAt: h(1), Requests: 10, Tokens: 1000, Quota: 500000},
		{Model: "model-b", CreatedAt: h(1), Requests: 5, Tokens: 500, Quota: 1500000},
		{Model: "model-a", CreatedAt: h(9), Requests: 5, Tokens: 500, Quota: 500000},
		{Model: "model-a", CreatedAt: h(-2), Requests: 99, Tokens: 99, Quota: 99}, // before the period
	}
	u := Aggregate(rows, p, fixedNow, usd())
	if u.Requests != 20 || u.Tokens != 2000 || u.Spend != 5 {
		t.Fatalf("totals = %v requests, %v tokens, %v spend", u.Requests, u.Tokens, u.Spend)
	}
	if len(u.Models) != 2 || u.Models[0].Model != "model-b" || u.Models[0].Spend != 3 || u.Models[0].Share != 0.6 ||
		u.Models[1].Model != "model-a" || u.Models[1].Spend != 2 || u.Models[1].Requests != 15 || u.Models[1].Share != 0.4 {
		t.Fatalf("models = %+v", u.Models)
	}
	// 10:30 into the day: 630 minutes, eleven hourly buckets.
	near(t, "avg rpm", u.AvgRPM, 20.0/630)
	near(t, "avg tpm", u.AvgTPM, 2000.0/630)
	if len(u.Buckets) != 11 {
		t.Fatalf("buckets = %d", len(u.Buckets))
	}
	b1, b9 := u.Buckets[1], u.Buckets[9]
	if !b1.Start.Equal(p.Start.Add(time.Hour)) || b1.Spend[0] != 3 || b1.Spend[1] != 1 || b1.Requests[1] != 10 || b9.Spend[1] != 1 || b9.Tokens[1] != 500 {
		t.Fatalf("bucket 1 = %+v, bucket 9 = %+v", b1, b9)
	}

	// A finished period divides by its whole length.
	y := resolve(t, fixedNow, PresetYesterday, "", "")
	yu := Aggregate([]UsageRow{{Model: "m", CreatedAt: y.Start.Unix(), Requests: 1440, Tokens: 2880}}, y, fixedNow, usd())
	near(t, "yesterday avg rpm", yu.AvgRPM, 1)
	near(t, "yesterday avg tpm", yu.AvgTPM, 2)
	if len(yu.Buckets) != 24 {
		t.Fatalf("yesterday buckets = %d", len(yu.Buckets))
	}

	// Daily buckets follow local dates.
	w := resolve(t, fixedNow, Preset7d, "", "")
	wu := Aggregate([]UsageRow{{Model: "m", CreatedAt: w.Start.Add(47 * time.Hour).Unix(), Requests: 3}}, w, fixedNow, usd())
	if len(wu.Buckets) != 7 || wu.Buckets[1].Requests[0] != 3 {
		t.Fatalf("7d buckets = %+v", wu.Buckets)
	}

	// Nothing spent: shares stay zero, not NaN.
	free := Aggregate([]UsageRow{{Model: "free", CreatedAt: h(2), Requests: 4}}, p, fixedNow, usd())
	if free.Models[0].Share != 0 || free.Spend != 0 || free.Requests != 4 {
		t.Fatalf("free = %+v", free)
	}
}

func TestUsageRowsMissingOptionalFields(t *testing.T) {
	f := newFakeSite(t)
	start := time.Date(2026, 3, 15, 0, 0, 0, 0, fixedNow.Location()).Unix()
	f.on(EndpointData, func(w http.ResponseWriter, _ *http.Request) {
		ok(w, []map[string]any{
			// Older sites: only the five fields, as floats.
			{"model_name": "model-a", "created_at": float64(start + 3600), "count": 2.0, "token_used": 30.0, "quota": 500000.0},
			// Newer sites add fields the plugin does not read.
			{"model_name": "model-a", "created_at": start + 7200, "count": 1, "token_used": 10, "quota": 0, "use_group": "default", "token_id": 7, "channel_id": 3, "node_name": "n1", "username": "u"},
			// Counters missing.
			{"model_name": "model-b", "created_at": start + 7200, "quota": 250000},
			// No model name and no time: counted in the totals only.
			{"count": 4, "token_used": 40, "quota": 250000},
		})
	})
	r := source(fixedNow).Fetch(context.Background(), account(f, testPAT))
	sum := SummarizePeriod(r.TodayPeriod(), resolve(t, fixedNow, PresetToday, "", ""))
	if sum.UsageFailure != nil || sum.Usage == nil {
		t.Fatalf("usage failure = %+v", sum.UsageFailure)
	}
	u := sum.Usage
	if u.Requests != 7 || u.Tokens != 80 || u.Spend != 2 || len(u.Models) != 3 {
		t.Fatalf("usage = %+v", u)
	}
	var bucketed float64
	for _, b := range u.Buckets {
		for _, n := range b.Requests {
			bucketed += n
		}
	}
	if bucketed != 3 {
		t.Fatalf("bucketed requests = %v, want 3 (the undated row is in no bucket)", bucketed)
	}
	if s := Summarize(r, nil); s.State != status.OK {
		t.Fatalf("state = %s", s.State)
	}
}

func TestPeriodSummaryFailures(t *testing.T) {
	successFalse := respond(200, "application/json", `{"success":false,"message":"range too long"}`)
	week := resolve(t, fixedNow, Preset7d, "", "")
	cases := []struct {
		name  string
		setup func(f *fakeSite)
		check func(t *testing.T, s PeriodSummary)
	}{
		{"site rejects the period", func(f *fakeSite) {
			f.on(EndpointStat, successFalse)
			f.on(EndpointData, successFalse)
		}, func(t *testing.T, s PeriodSummary) {
			if s.Spend != nil || s.SpendFailure == nil || s.SpendFailure.Code != status.BadResponse || s.SpendFailure.Diagnostic != "SUCCESS_FALSE" {
				t.Fatalf("spend = %v failure = %+v", deref(s.Spend), s.SpendFailure)
			}
			if s.Usage != nil || s.UsageFailure == nil || s.UsageFailure.Code != status.BadResponse || s.UsageDisabled {
				t.Fatalf("usage failure = %+v disabled = %v", s.UsageFailure, s.UsageDisabled)
			}
		}},
		{"dashboard disabled is not a failure", func(f *fakeSite) {
			f.setStatus(map[string]any{"quota_per_unit": 500000, "quota_display_type": "USD", "enable_data_export": false})
			f.on(EndpointData, respond(500, "application/json", `{"success":false}`))
		}, func(t *testing.T, s PeriodSummary) {
			if !s.UsageDisabled || s.UsageFailure != nil || s.Usage != nil || s.SpendFailure != nil {
				t.Fatalf("summary = %+v", s)
			}
			near(t, "spend", s.Spend, 0.5)
		}},
		{"usage endpoint fails with the dashboard on", func(f *fakeSite) { f.on(EndpointData, respond(502, "text/plain", "bad gateway")) },
			func(t *testing.T, s PeriodSummary) {
				if s.UsageDisabled || s.UsageFailure == nil || s.UsageFailure.Code != status.SiteError {
					t.Fatalf("summary = %+v", s)
				}
				near(t, "spend", s.Spend, 0.5)
			}},
		{"no site settings: nothing can be converted", func(f *fakeSite) { f.on(EndpointStatus, respond(503, "text/plain", "down")) },
			func(t *testing.T, s PeriodSummary) {
				if s.Spend != nil || s.Usage != nil || s.SpendFailure == nil || s.SpendFailure.Code != status.SiteError || s.UsageFailure == nil || s.UsageFailure.Code != status.SiteError {
					t.Fatalf("summary = %+v", s)
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSite(t)
			tc.setup(f)
			r := source(fixedNow).FetchPeriod(context.Background(), account(f, testPAT), week)
			s := SummarizePeriod(r, week)
			if !s.IncludesNow || s.Minutes <= 0 {
				t.Fatalf("includes now = %v, minutes = %v", s.IncludesNow, s.Minutes)
			}
			tc.check(t, s)
		})
	}
}
