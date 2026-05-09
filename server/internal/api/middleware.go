package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// IPRateLimit wraps a handler with a per-IP token bucket. Useful for /login
// where unauthenticated traffic is exposed to the public internet.
//
// burst:  initial bucket size (also the max)
// refill: tokens added per second
func IPRateLimit(burst, refill float64) func(http.Handler) http.Handler {
	rl := &ipLimiter{
		burst:   burst,
		refill:  refill,
		buckets: map[string]*ipBucket{},
		now:     time.Now,
	}
	// Background sweep so old IPs don't accumulate forever.
	go rl.sweepLoop()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			if !rl.allow(ip) {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "rate limited", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type ipLimiter struct {
	mu      sync.Mutex
	burst   float64
	refill  float64
	buckets map[string]*ipBucket
	now     func() time.Time
}

type ipBucket struct {
	tokens float64
	last   time.Time
}

func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[ip]
	if !ok {
		b = &ipBucket{tokens: l.burst, last: now}
		l.buckets[ip] = b
	}
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * l.refill
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *ipLimiter) sweepLoop() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for range t.C {
		l.sweep()
	}
}

func (l *ipLimiter) sweep() {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := l.now().Add(-15 * time.Minute)
	for ip, b := range l.buckets {
		if b.last.Before(cutoff) {
			delete(l.buckets, ip)
		}
	}
}

// clientIP extracts a usable client IP, honoring X-Forwarded-For when present
// (Caddy/nginx will populate it). Falls back to RemoteAddr.
func clientIP(r *http.Request) string {
	if h := r.Header.Get("X-Forwarded-For"); h != "" {
		// XFF can be a comma-separated chain; the first is the originating client.
		if comma := strings.Index(h, ","); comma >= 0 {
			return strings.TrimSpace(h[:comma])
		}
		return strings.TrimSpace(h)
	}
	if h := r.Header.Get("X-Real-IP"); h != "" {
		return h
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
