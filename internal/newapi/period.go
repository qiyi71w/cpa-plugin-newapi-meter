package newapi

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/status"
	"github.com/qiyi71w/cpa-plugin-newapi-meter/internal/store"
)

// Period presets.
const (
	PresetToday     = "today"
	PresetYesterday = "yesterday"
	Preset7d        = "7d"
	Preset30d       = "30d"
	PresetCustom    = "custom"
)

// Bucket granularities.
const (
	GranularityHour = "hour"
	GranularityDay  = "day"
)

// MaxPeriodDays is the longest period, counting both end days.
const MaxPeriodDays = 365

// hourlyMaxDays is the longest period bucketed by hour; longer ones use local days.
const hourlyMaxDays = 2

// Reasons a custom period is rejected, besides store.ReasonRequired,
// store.ReasonInvalid and store.ReasonUnsupported.
const (
	ReasonSwapped = "swapped"  // end date before start date
	ReasonTooLong = "too_long" // more than MaxPeriodDays days
	ReasonFuture  = "future"   // a date after today
)

const dateLayout = "2006-01-02"

// PeriodError is an invalid period parameter; Field is "period", "start" or "end".
type PeriodError struct {
	Field  string
	Reason string
}

func (e *PeriodError) Error() string { return "period " + e.Field + ": " + e.Reason }

// Period is a whole number of local calendar days in CPA's time zone.
type Period struct {
	Preset string
	// StartDate and EndDate are the first and last day, both included (YYYY-MM-DD).
	StartDate, EndDate string
	Days               int
	// Start is local midnight of the first day; End is local midnight after
	// the last day, so a period that includes today ends in the future.
	Start, End  time.Time
	Granularity string
}

// ResolvePeriod turns a preset, or a custom start/end date pair, into a
// period in now's zone. Today is the local day containing now.
func ResolvePeriod(now time.Time, preset, start, end string) (Period, error) {
	today := LocalMidnight(now)
	var first, last time.Time
	switch preset {
	case "", PresetToday:
		preset, first, last = PresetToday, today, today
	case PresetYesterday:
		first = addDays(today, -1)
		last = first
	case Preset7d:
		first, last = addDays(today, -6), today
	case Preset30d:
		first, last = addDays(today, -29), today
	case PresetCustom:
		var err error
		if first, err = parseDate(start, "start", now.Location()); err != nil {
			return Period{}, err
		}
		if last, err = parseDate(end, "end", now.Location()); err != nil {
			return Period{}, err
		}
		switch {
		case civilDay(first) > civilDay(today):
			return Period{}, &PeriodError{"start", ReasonFuture}
		case civilDay(last) > civilDay(today):
			return Period{}, &PeriodError{"end", ReasonFuture}
		case civilDay(last) < civilDay(first):
			return Period{}, &PeriodError{"end", ReasonSwapped}
		case civilDay(last)-civilDay(first)+1 > MaxPeriodDays:
			return Period{}, &PeriodError{"end", ReasonTooLong}
		}
	default:
		return Period{}, &PeriodError{"period", store.ReasonUnsupported}
	}
	p := Period{
		Preset:    preset,
		StartDate: first.Format(dateLayout),
		EndDate:   last.Format(dateLayout),
		Days:      int(civilDay(last)-civilDay(first)) + 1,
		Start:     first,
		End:       addDays(last, 1),
	}
	p.Granularity = GranularityDay
	if p.Days <= hourlyMaxDays {
		p.Granularity = GranularityHour
	}
	return p, nil
}

func parseDate(s, field string, loc *time.Location) (time.Time, error) {
	if s == "" {
		return time.Time{}, &PeriodError{field, store.ReasonRequired}
	}
	t, err := time.ParseInLocation(dateLayout, s, loc)
	if err != nil {
		return time.Time{}, &PeriodError{field, store.ReasonInvalid}
	}
	return LocalMidnight(t), nil
}

// addDays moves a local midnight by n calendar days; across a DST change the
// result is still local midnight, not a multiple of 24 hours away.
func addDays(midnight time.Time, n int) time.Time {
	y, m, d := midnight.Date()
	return time.Date(y, m, d+n, 0, 0, 0, 0, midnight.Location())
}

// civilDay numbers t's local calendar date.
func civilDay(t time.Time) int64 {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400
}

// IsToday reports whether the period is exactly the local day containing now;
// that period reuses the overview's today query.
func (p Period) IsToday(now time.Time) bool {
	d := LocalMidnight(now).Format(dateLayout)
	return p.StartDate == d && p.EndDate == d
}

// IncludesNow reports whether at falls inside the period.
func (p Period) IncludesNow(at time.Time) bool { return at.Before(p.End) }

// until is where the period's data stops at time at: End, or at while the
// period is still running.
func (p Period) until(at time.Time) time.Time {
	if p.IncludesNow(at) {
		return at
	}
	return p.End
}

// Minutes is the average RPM/TPM denominator: the period's length in
// minutes, cut off at at while the period is still running.
func (p Period) Minutes(at time.Time) float64 {
	return max(0, p.until(at).Sub(p.Start).Minutes())
}

// queryEnd is the inclusive end_timestamp for site queries made at time at.
func (p Period) queryEnd(at time.Time) time.Time {
	if p.IncludesNow(at) {
		return at
	}
	return p.End.Add(-time.Second)
}

// bucketStarts lists the start of each bucket up to at: local hours (23 or 25
// on DST change days) or local calendar days.
func (p Period) bucketStarts(at time.Time) []time.Time {
	stop := p.until(at)
	var out []time.Time
	for t := p.Start; t.Before(stop); {
		out = append(out, t)
		if p.Granularity == GranularityHour {
			t = t.Add(time.Hour)
		} else {
			t = addDays(t, 1)
		}
	}
	return out
}

// UsageRow is one /api/data/self row. Only these five fields are read; every
// one may be missing (numbers count as zero, a missing model is "", a missing
// created_at puts the row in the totals but in no bucket).
type UsageRow struct {
	Model     string
	CreatedAt int64 // Unix seconds; 0 when missing
	Requests  float64
	Tokens    float64
	Quota     float64
}

func parseRows(data json.RawMessage) ([]UsageRow, *status.Failure) {
	var raw []struct {
		ModelName *string  `json:"model_name"`
		CreatedAt *float64 `json:"created_at"`
		Count     *float64 `json:"count"`
		TokenUsed *float64 `json:"token_used"`
		Quota     *float64 `json:"quota"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return nil, missingFields()
	}
	val := func(p *float64) float64 {
		if p == nil {
			return 0
		}
		return *p
	}
	rows := make([]UsageRow, len(raw))
	for i, r := range raw {
		if r.ModelName != nil {
			rows[i].Model = *r.ModelName
		}
		rows[i].CreatedAt = int64(val(r.CreatedAt))
		rows[i].Requests, rows[i].Tokens, rows[i].Quota = val(r.Count), val(r.TokenUsed), val(r.Quota)
	}
	return rows, nil
}

// ModelUsage is one model's share of a period, in display units.
type ModelUsage struct {
	Model    string  `json:"model"`
	Spend    float64 `json:"spend"`
	Share    float64 `json:"share"` // of the period's spend, 0..1; 0 when nothing was spent
	Requests float64 `json:"requests"`
	Tokens   float64 `json:"tokens"`
}

// Bucket is one hour or day. Its slices are per model, in Usage.Models order.
type Bucket struct {
	Start    time.Time `json:"start"`
	Spend    []float64 `json:"spend"`
	Requests []float64 `json:"requests"`
	Tokens   []float64 `json:"tokens"`
}

// Usage is a period's /api/data/self rows aggregated, in display units.
type Usage struct {
	Spend    float64 `json:"spend"`
	Requests float64 `json:"requests"`
	Tokens   float64 `json:"tokens"`
	// AvgRPM and AvgTPM are per minute of the period (cut off at the fetch
	// time while it runs); nil before the period's first minute has passed.
	AvgRPM *float64 `json:"avg_rpm"`
	AvgTPM *float64 `json:"avg_tpm"`
	// Models is sorted by spend, then requests, then name.
	Models  []ModelUsage `json:"models"`
	Buckets []Bucket     `json:"buckets"`
}

// Aggregate sums rows inside p by model and bucket as of time at, converting
// quota with site. Rows dated outside the period are ignored.
func Aggregate(rows []UsageRow, p Period, at time.Time, site *Site) Usage {
	starts := p.bucketStarts(at)
	from, to := p.Start.Unix(), p.End.Unix()
	type cell struct {
		model, bucket int
		row           *UsageRow
	}
	var (
		index  = map[string]int{}
		models []ModelUsage
		quota  []float64
		cells  []cell
	)
	for i := range rows {
		r := &rows[i]
		if r.CreatedAt != 0 && (r.CreatedAt < from || r.CreatedAt >= to) {
			continue
		}
		m, ok := index[r.Model]
		if !ok {
			m = len(models)
			index[r.Model] = m
			models = append(models, ModelUsage{Model: r.Model})
			quota = append(quota, 0)
		}
		models[m].Requests += r.Requests
		models[m].Tokens += r.Tokens
		quota[m] += r.Quota
		if r.CreatedAt == 0 {
			continue
		}
		if b := sort.Search(len(starts), func(i int) bool { return starts[i].Unix() > r.CreatedAt }) - 1; b >= 0 {
			cells = append(cells, cell{m, b, r})
		}
	}

	order := make([]int, len(models))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(x, y int) bool {
		a, b := order[x], order[y]
		if quota[a] != quota[b] {
			return quota[a] > quota[b]
		}
		if models[a].Requests != models[b].Requests {
			return models[a].Requests > models[b].Requests
		}
		return models[a].Model < models[b].Model
	})
	rank := make([]int, len(models))
	var u Usage
	var totalQuota float64
	for _, q := range quota {
		totalQuota += q
	}
	u.Models = make([]ModelUsage, len(models))
	for pos, m := range order {
		rank[m] = pos
		mu := models[m]
		mu.Spend = site.Convert(quota[m])
		if totalQuota > 0 {
			mu.Share = quota[m] / totalQuota
		}
		u.Models[pos] = mu
		u.Requests += mu.Requests
		u.Tokens += mu.Tokens
	}
	u.Spend = site.Convert(totalQuota)

	n := len(models)
	u.Buckets = make([]Bucket, len(starts))
	for i, s := range starts {
		u.Buckets[i] = Bucket{Start: s, Spend: make([]float64, n), Requests: make([]float64, n), Tokens: make([]float64, n)}
	}
	for _, c := range cells {
		b, m := &u.Buckets[c.bucket], rank[c.model]
		b.Spend[m] += site.Convert(c.row.Quota)
		b.Requests[m] += c.row.Requests
		b.Tokens[m] += c.row.Tokens
	}

	if minutes := p.Minutes(at); minutes > 0 {
		rpm, tpm := u.Requests/minutes, u.Tokens/minutes
		u.AvgRPM, u.AvgTPM = &rpm, &tpm
	}
	return u
}

// PeriodReading is the raw result of the queries behind one period.
type PeriodReading struct {
	Site *Site
	// Probes lists /api/status, /api/log/self/stat and /api/data/self in that order.
	Probes     []status.Probe
	SpendQuota *float64 // log/self/stat quota for the period
	Rows       []UsageRow
	FetchedAt  time.Time
}

func (r PeriodReading) failure(endpoint string) *status.Failure {
	for _, p := range r.Probes {
		if p.Name == endpoint {
			return p.Failure
		}
	}
	return nil
}

// TodayPeriod is today's part of an overview reading, so today's period
// reuses the overview query instead of asking the site again.
func (r Reading) TodayPeriod() PeriodReading {
	pr := PeriodReading{Site: r.Site, SpendQuota: r.TodayQuota, Rows: r.Usage, FetchedAt: r.FetchedAt}
	for _, p := range r.Probes {
		if p.Name != EndpointSelf {
			pr.Probes = append(pr.Probes, p)
		}
	}
	return pr
}

// FetchPeriod queries the site settings, the period's spend and its usage
// rows concurrently. Like Fetch it never returns an error.
func (s *Source) FetchPeriod(ctx context.Context, a store.Account, p Period) PeriodReading {
	now := s.Now()
	pr := PeriodReading{FetchedAt: now}
	q := url.Values{}
	q.Set("start_timestamp", strconv.FormatInt(p.Start.Unix(), 10))
	q.Set("end_timestamp", strconv.FormatInt(p.queryEnd(now).Unix(), 10))
	statQ := url.Values{"type": {"2"}}
	for k, v := range q {
		statQ[k] = v
	}

	var (
		wg                             sync.WaitGroup
		statusData, statData, rowsData json.RawMessage
		fStatus, fStat, fData          *status.Failure
	)
	wg.Add(3)
	go func() { defer wg.Done(); statusData, fStatus = s.get(ctx, a, EndpointStatus, "", false) }()
	go func() { defer wg.Done(); statData, fStat = s.get(ctx, a, EndpointStat, statQ.Encode(), true) }()
	go func() { defer wg.Done(); rowsData, fData = s.get(ctx, a, EndpointData, q.Encode(), true) }()
	wg.Wait()

	if fStatus == nil {
		pr.Site, fStatus = parseSite(statusData)
	}
	if fStat == nil {
		var v struct {
			Quota *float64 `json:"quota"`
		}
		if json.Unmarshal(statData, &v) != nil || v.Quota == nil {
			fStat = missingFields()
		} else {
			pr.SpendQuota = v.Quota
		}
	}
	if fData == nil {
		pr.Rows, fData = parseRows(rowsData)
	}
	pr.Probes = []status.Probe{
		{Name: EndpointStatus, Failure: fStatus},
		{Name: EndpointStat, Credentialed: true, Failure: fStat},
		{Name: EndpointData, Credentialed: true, Failure: fData},
	}
	return pr
}

// PeriodSummary is what the account detail shows for one period. It never
// carries an account status: that is decided from today's overview alone.
type PeriodSummary struct {
	Currency   *Currency
	DataExport *bool
	// Spend is the period's spend from /api/log/self/stat (real time).
	Spend        *float64
	SpendFailure *status.Failure
	// Usage comes from /api/data/self (about five minutes behind).
	Usage        *Usage
	UsageFailure *status.Failure
	// UsageDisabled: the site reports enable_data_export=false, so it keeps
	// no usage data; this is not a failure.
	UsageDisabled bool
	IncludesNow   bool
	Minutes       float64
	FetchedAt     time.Time
}

// SummarizePeriod converts a period reading into display units. Without the
// site settings no amount can be converted, so the settings failure stands
// in for both parts.
func SummarizePeriod(r PeriodReading, p Period) PeriodSummary {
	out := PeriodSummary{IncludesNow: p.IncludesNow(r.FetchedAt), Minutes: p.Minutes(r.FetchedAt), FetchedAt: r.FetchedAt}
	fStatus, fStat, fData := r.failure(EndpointStatus), r.failure(EndpointStat), r.failure(EndpointData)
	if r.Site == nil {
		out.SpendFailure, out.UsageFailure = fStat, fData
		if fStat == nil {
			out.SpendFailure = fStatus
		}
		if fData == nil {
			out.UsageFailure = fStatus
		}
		return out
	}
	c := r.Site.Currency
	out.Currency, out.DataExport = &c, r.Site.DataExport
	if fStat != nil {
		out.SpendFailure = fStat
	} else if r.SpendQuota != nil {
		v := r.Site.Convert(*r.SpendQuota)
		out.Spend = &v
	}
	switch {
	case r.Site.DataExport != nil && !*r.Site.DataExport:
		out.UsageDisabled = true
	case fData != nil:
		out.UsageFailure = fData
	default:
		u := Aggregate(r.Rows, p, r.FetchedAt, r.Site)
		out.Usage = &u
	}
	return out
}
