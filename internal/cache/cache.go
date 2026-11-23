// Package cache keeps each account's query results in memory for a short
// time and merges concurrent queries for the same account into one fetch.
package cache

import (
	"sync"
	"time"
)

// TTL is how long a result is served without asking the site again.
const TTL = 60 * time.Second

// Key identifies one cached query.
type Key struct {
	// Account is the account ID; Forget drops every query of an account.
	Account string
	// Query names the query and its parameters (for example "overview").
	Query string
	// Fingerprint changes whenever an input of the fetch changes (site,
	// credential, proxy option); a different fingerprint is a miss.
	Fingerprint string
}

type flight[V any] struct {
	done chan struct{}
	val  V
	at   time.Time
}

type slot[V any] struct {
	fingerprint string
	has         bool
	val         V
	at          time.Time
	inflight    *flight[V]
}

// Cache is safe for concurrent use.
type Cache[V any] struct {
	ttl   time.Duration
	now   func() time.Time
	mu    sync.Mutex
	slots map[[2]string]*slot[V]
}

// New returns a cache with the given TTL and clock.
func New[V any](ttl time.Duration, now func() time.Time) *Cache[V] {
	return &Cache[V]{ttl: ttl, now: now, slots: map[[2]string]*slot[V]{}}
}

// Get returns a fresh cached value, or runs fetch. While a fetch for the key
// is running, every caller (forced or not) waits for it and shares its result
// instead of starting another. force skips a fresh cached value. at is when
// the returned value was fetched; hit reports that no fetch ran for this call.
func (c *Cache[V]) Get(key Key, force bool, fetch func() V) (v V, at time.Time, hit bool) {
	id := [2]string{key.Account, key.Query}
	c.mu.Lock()
	s := c.slots[id]
	if s == nil || s.fingerprint != key.Fingerprint {
		s = &slot[V]{fingerprint: key.Fingerprint}
		c.slots[id] = s
	}
	if f := s.inflight; f != nil {
		c.mu.Unlock()
		<-f.done
		return f.val, f.at, false
	}
	if !force && s.has && c.now().Sub(s.at) < c.ttl {
		v, at := s.val, s.at
		c.mu.Unlock()
		return v, at, true
	}
	f := &flight[V]{done: make(chan struct{})}
	s.inflight = f
	c.mu.Unlock()

	completed := false
	defer func() {
		c.mu.Lock()
		s.inflight = nil
		if completed && c.slots[id] == s {
			s.has, s.val, s.at = true, f.val, f.at
		}
		c.mu.Unlock()
		close(f.done)
	}()
	f.val = fetch()
	f.at = c.now()
	completed = true
	return f.val, f.at, false
}

// Forget drops every cached query of an account.
func (c *Cache[V]) Forget(account string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id := range c.slots {
		if id[0] == account {
			delete(c.slots, id)
		}
	}
}
