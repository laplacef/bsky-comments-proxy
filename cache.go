package main

import (
	"sync"
	"time"
)

type entry struct {
	data  []byte
	ctype string
	exp   time.Time
}

type call struct {
	done  chan struct{}
	data  []byte
	ctype string
	err   error
}

// cache is a TTL map with single-flight fills: concurrent misses on the same
// key wait for one upstream call instead of stampeding.
type cache struct {
	mu       sync.Mutex
	ttl      time.Duration
	max      int
	entries  map[string]*entry
	inflight map[string]*call
}

func newCache(ttl time.Duration, max int) *cache {
	return &cache{
		ttl:      ttl,
		max:      max,
		entries:  make(map[string]*entry),
		inflight: make(map[string]*call),
	}
}

func (c *cache) get(key string, fill func() ([]byte, string, error)) ([]byte, string, error) {
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && time.Now().Before(e.exp) {
		c.mu.Unlock()
		return e.data, e.ctype, nil
	}
	if w, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		<-w.done
		return w.data, w.ctype, w.err
	}
	w := &call{done: make(chan struct{})}
	c.inflight[key] = w
	c.mu.Unlock()

	w.data, w.ctype, w.err = fill()
	close(w.done)

	c.mu.Lock()
	delete(c.inflight, key)
	if w.err == nil {
		if len(c.entries) >= c.max {
			c.evict()
		}
		c.entries[key] = &entry{data: w.data, ctype: w.ctype, exp: time.Now().Add(c.ttl)}
	}
	c.mu.Unlock()
	return w.data, w.ctype, w.err
}

// evict drops expired entries, then arbitrary ones until under the cap.
// Map iteration order is random, which is good enough here.
func (c *cache) evict() {
	now := time.Now()
	for k, e := range c.entries {
		if now.After(e.exp) {
			delete(c.entries, k)
		}
	}
	for k := range c.entries {
		if len(c.entries) < c.max {
			break
		}
		delete(c.entries, k)
	}
}
