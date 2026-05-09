package ws

import (
	"sync"
	"time"
)

// rateLimiter is a tiny per-key token-bucket. Used to throttle abusive
// clients sending action/rebuy/chat at a high rate.
//
// The bucket holds at most `burst` tokens and refills at `refill` tokens per
// second. Allow returns false when the request would exceed the bucket.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	burst   float64
	refill  float64
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(burst, refill float64) *rateLimiter {
	return &rateLimiter{
		buckets: map[string]*bucket{},
		burst:   burst,
		refill:  refill,
		now:     time.Now,
	}
}

// Allow consumes one token from the bucket for `key`. Returns true when the
// caller may proceed.
func (r *rateLimiter) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	b, ok := r.buckets[key]
	if !ok {
		b = &bucket{tokens: r.burst, last: now}
		r.buckets[key] = b
	}
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens = minF(r.burst, b.tokens+elapsed*r.refill)
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Forget removes a key's bucket. Hub calls this on disconnect.
func (r *rateLimiter) Forget(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.buckets, key)
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
