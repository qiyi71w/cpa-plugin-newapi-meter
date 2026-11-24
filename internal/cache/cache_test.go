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
