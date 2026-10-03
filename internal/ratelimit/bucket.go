// Package ratelimit implements a token bucket with an injectable clock.
package ratelimit

import (
	"sync"
	"time"
)

// Bucket is a token bucket that starts full. It is safe for concurrent use.
type Bucket struct {
	mu     sync.Mutex
	rate   float64 // tokens added per second
	burst  float64
	tokens float64
	last   time.Time
	now    func() time.Time
}

// NewBucket returns a full bucket refilling at rate tokens per second up to burst.
func NewBucket(rate float64, burst int, now func() time.Time) *Bucket {
	if burst < 1 {
		burst = 1
	}
	if now == nil {
		now = time.Now
	}
	return &Bucket{rate: rate, burst: float64(burst), tokens: float64(burst), last: now(), now: now}
}

// Allow takes one token. When none is available it returns false and the wait until one is.
func (b *Bucket) Allow() (bool, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens = min(b.burst, b.tokens+elapsed*b.rate)
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := (1 - b.tokens) / b.rate
	return false, time.Duration(wait * float64(time.Second))
}
