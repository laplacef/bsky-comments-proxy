package main

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fillCounter(calls *atomic.Int32, data string) func() ([]byte, string, error) {
	return func() ([]byte, string, error) {
		calls.Add(1)
		return []byte(data), "text/plain", nil
	}
}

func TestCacheServesFromCacheWithinTTL(t *testing.T) {
	c := newCache(time.Minute, 10)
	var calls atomic.Int32
	for range 3 {
		data, _, err := c.get("k", fillCounter(&calls, "v"))
		if err != nil || string(data) != "v" {
			t.Fatalf("get returned %q, %v", data, err)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("fill ran %d times, want 1", n)
	}
}

func TestCacheRefillsAfterTTL(t *testing.T) {
	c := newCache(30*time.Millisecond, 10)
	var calls atomic.Int32
	c.get("k", fillCounter(&calls, "v"))
	time.Sleep(60 * time.Millisecond)
	c.get("k", fillCounter(&calls, "v"))
	if n := calls.Load(); n != 2 {
		t.Fatalf("fill ran %d times, want 2", n)
	}
}

func TestCacheSingleFlight(t *testing.T) {
	c := newCache(time.Minute, 10)
	var calls atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			data, _, err := c.get("k", func() ([]byte, string, error) {
				calls.Add(1)
				time.Sleep(20 * time.Millisecond)
				return []byte("v"), "text/plain", nil
			})
			if err != nil || string(data) != "v" {
				t.Errorf("get returned %q, %v", data, err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("fill ran %d times, want 1", n)
	}
}

func TestCacheDoesNotCacheErrors(t *testing.T) {
	c := newCache(time.Minute, 10)
	var calls atomic.Int32
	boom := errors.New("boom")
	fail := func() ([]byte, string, error) {
		calls.Add(1)
		return nil, "", boom
	}
	if _, _, err := c.get("k", fail); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if _, _, err := c.get("k", fail); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("fill ran %d times, want 2", n)
	}
}

func TestCacheEvictsAtCap(t *testing.T) {
	c := newCache(time.Minute, 2)
	var calls atomic.Int32
	for _, k := range []string{"a", "b", "c", "d"} {
		c.get(k, fillCounter(&calls, k))
	}
	c.mu.Lock()
	n := len(c.entries)
	c.mu.Unlock()
	if n > 2 {
		t.Fatalf("cache holds %d entries, cap is 2", n)
	}
}
