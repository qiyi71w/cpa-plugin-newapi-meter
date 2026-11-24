package cache

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newCache() (*Cache[int], *clock) {
	c := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	return New[int](TTL, c.now), c
}

func TestSixtySecondCache(t *testing.T) {
	c, clk := newCache()
	var calls atomic.Int32
	fetch := func() int { return int(calls.Add(1)) }
	key := Key{Account: "a", Query: "overview", Fingerprint: "f"}

	if v, _, hit := c.Get(key, false, fetch); v != 1 || hit {
		t.Fatalf("first get = %d hit=%v", v, hit)
	}
	clk.add(59 * time.Second)
	if v, _, hit := c.Get(key, false, fetch); v != 1 || !hit {
		t.Fatalf("within 60 s = %d hit=%v", v, hit)
	}
	clk.add(time.Second)
	if v, _, hit := c.Get(key, false, fetch); v != 2 || hit {
		t.Fatalf("after 60 s = %d hit=%v", v, hit)
	}
	if v, _, hit := c.Get(key, true, fetch); v != 3 || hit {
		t.Fatalf("forced = %d hit=%v", v, hit)
	}
	if v, _, _ := c.Get(Key{Account: "a", Query: "overview", Fingerprint: "changed"}, false, fetch); v != 4 {
		t.Fatalf("changed inputs served stale value %d", v)
	}
	if v, _, _ := c.Get(Key{Account: "b", Query: "overview", Fingerprint: "f"}, false, fetch); v != 5 {
		t.Fatalf("other account shared a value: %d", v)
	}
	c.Forget("a")
	if v, _, _ := c.Get(Key{Account: "a", Query: "overview", Fingerprint: "changed"}, false, fetch); v != 6 {
		t.Fatalf("forgotten account served cached value %d", v)
	}
}

func TestConcurrentQueriesShareOneFetch(t *testing.T) {
	c, _ := newCache()
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	fetch := func() int {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return 7
	}
	key := Key{Account: "a", Query: "overview", Fingerprint: "f"}
	const n = 16
	results := make(chan int, n)
	go func() { v, _, _ := c.Get(key, true, fetch); results <- v }()
	<-started
	var wg sync.WaitGroup
	for i := 1; i < n; i++ {
		wg.Add(1)
		go func(force bool) {
			defer wg.Done()
			v, _, _ := c.Get(key, force, fetch)
			results <- v
		}(i%2 == 0)
	}
	// Give the joiners time to reach the in-flight fetch before releasing it.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	for i := range n {
		if v := <-results; v != 7 {
			t.Fatalf("result %d = %d", i, v)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fetches = %d, want 1", got)
	}
}

func (c *Cache[V]) slotsOf(account string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for id := range c.slots {
		if id[0] == account {
			n++
		}
	}
	return n
}

// The plugin keys today's queries by local date and periods by their date
// range, so each day of use brings new keys. Results the cache can no longer
// serve are dropped: the slot count follows one day's use, not the days run.
func TestPastResultsDoNotAccumulate(t *testing.T) {
	c, clk := newCache()
	var calls atomic.Int32
	fetch := func() int { return int(calls.Add(1)) }
	day := func(d time.Time, n int) string { return d.AddDate(0, 0, n).Format(time.DateOnly) }
	useDay := func() int {
		today := clk.now()
		queries := []string{
			"overview:" + day(today, 0),
			"custom:" + day(today, 0),
			"period:" + day(today, -1) + ":" + day(today, -1),
			"period:" + day(today, -6) + ":" + day(today, 0),
			"period:2025-12-01:2025-12-05", // a fixed range viewed every day
		}
		for _, q := range queries {
			c.Get(Key{Account: "a", Query: q, Fingerprint: "f"}, false, fetch)
		}
		return len(queries)
	}

	clk.add(9 * time.Hour)
	perDay := useDay()
	counts, bounded := []int{c.slotsOf("a")}, true
	for range 30 {
		clk.add(24 * time.Hour)
		useDay()
		counts = append(counts, c.slotsOf("a"))
		bounded = bounded && counts[len(counts)-1] == perDay
	}
	if !bounded {
		t.Fatalf("account a's cached queries per day over 31 days: %v; want %d every day (one day's use)", counts, perDay)
	}

	// A day without use leaves nothing behind once another query arrives.
	clk.add(24 * time.Hour)
	c.Get(Key{Account: "b", Query: "overview", Fingerprint: "f"}, false, fetch)
	if got := c.slotsOf("a"); got != 0 {
		t.Fatalf("idle account a still holds %d cached queries", got)
	}
}

// Dropping expired results never touches a result that can still be served
// or a fetch that is still running.
func TestPruningKeepsFreshResultsAndRunningFetches(t *testing.T) {
	c, clk := newCache()
	var calls atomic.Int32
	fetch := func() int { return int(calls.Add(1)) }
	fresh := Key{Account: "a", Query: "overview:2026-01-01", Fingerprint: "f"}
	slow := Key{Account: "a", Query: "period:2025-12-01:2025-12-31", Fingerprint: "f"}

	started, release := make(chan struct{}), make(chan struct{})
	slowDone := make(chan int, 2)
	go func() {
		v, _, _ := c.Get(slow, false, func() int { close(started); <-release; return 100 })
		slowDone <- v
	}()
	<-started

	// Queries of another account drive the sweeps; they fetch separately.
	other := func() int { return -1 }
	clk.add(2 * TTL)
	c.Get(Key{Account: "b", Query: "overview:2026-01-01", Fingerprint: "f"}, false, other)
	clk.add(TTL / 2)
	if v, _, hit := c.Get(fresh, false, fetch); v != 1 || hit {
		t.Fatalf("first get = %d hit=%v", v, hit)
	}
	clk.add(TTL / 2)
	// A sweep runs now; the fresh value (TTL/2 old) and the running fetch stay.
	c.Get(Key{Account: "b", Query: "overview:2026-01-02", Fingerprint: "f"}, false, other)
	if v, _, hit := c.Get(fresh, false, fetch); v != 1 || !hit {
		t.Fatalf("fresh value after a sweep = %d hit=%v", v, hit)
	}
	go func() { v, _, _ := c.Get(slow, true, fetch); slowDone <- v }()
	time.Sleep(50 * time.Millisecond)
	close(release)
	for range 2 {
		if v := <-slowDone; v != 100 {
			t.Fatalf("a caller of the running fetch got %d, want its shared result", v)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("fetches = %d, want 1 (the running fetch was not repeated)", calls.Load())
	}
}
