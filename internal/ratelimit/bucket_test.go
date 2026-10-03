package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestBucket_AllowsBurstThenRejects(t *testing.T) {
	c := &clock{t: time.Unix(0, 0)}
	b := NewBucket(1, 3, c.now)
	for i := range 3 {
		if ok, _ := b.Allow(); !ok {
			t.Fatalf("request %d rejected within burst", i)
		}
	}
	ok, wait := b.Allow()
	if ok {
		t.Fatal("request beyond burst was allowed")
	}
	if wait != time.Second {
		t.Fatalf("wait = %v, want 1s", wait)
	}
}

func TestBucket_RefillsAtRate(t *testing.T) {
	c := &clock{t: time.Unix(0, 0)}
	b := NewBucket(2, 1, c.now)
	b.Allow()
	if ok, _ := b.Allow(); ok {
		t.Fatal("second request allowed with burst 1")
	}
	c.t = c.t.Add(500 * time.Millisecond)
	if ok, _ := b.Allow(); !ok {
		t.Fatal("token not refilled after 1/rate seconds")
	}
}

func TestBucket_NeverExceedsBurstAfterIdle(t *testing.T) {
	c := &clock{t: time.Unix(0, 0)}
	b := NewBucket(10, 2, c.now)
	c.t = c.t.Add(time.Hour)
	allowed := 0
	for range 10 {
		if ok, _ := b.Allow(); ok {
			allowed++
		}
	}
	if allowed != 2 {
		t.Fatalf("allowed %d after idle, want burst 2", allowed)
	}
}

func TestBucket_ConcurrentCallersGetExactlyBurst(t *testing.T) {
	c := &clock{t: time.Unix(0, 0)}
	b := NewBucket(1, 10, c.now)
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := b.Allow(); ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 10 {
		t.Fatalf("allowed %d concurrent requests, want 10", allowed.Load())
	}
}
