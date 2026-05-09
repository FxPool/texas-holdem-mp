package ws

import (
	"testing"
	"time"
)

func TestRateLimiterAllowsBurstThenThrottles(t *testing.T) {
	rl := newRateLimiter(3, 1)
	now := time.Unix(0, 0)
	rl.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !rl.Allow("u1") {
			t.Fatalf("burst[%d] should be allowed", i)
		}
	}
	if rl.Allow("u1") {
		t.Errorf("4th request should be throttled")
	}

	// Advance 1.5s → 1.5 tokens added (capped at burst=3, but we drained to 0
	// so we get 1.5 tokens → 1 full request allowed, second denied)
	now = now.Add(1500 * time.Millisecond)
	if !rl.Allow("u1") {
		t.Errorf("after 1.5s should allow one")
	}
	if rl.Allow("u1") {
		t.Errorf("immediately after that should throttle")
	}
}

func TestRateLimiterPerKey(t *testing.T) {
	rl := newRateLimiter(1, 1)
	if !rl.Allow("a") {
		t.Errorf("a should pass")
	}
	if rl.Allow("a") {
		t.Errorf("a second request should throttle")
	}
	if !rl.Allow("b") {
		t.Errorf("b should pass independently")
	}
}

func TestRateLimiterForget(t *testing.T) {
	rl := newRateLimiter(1, 1)
	rl.Allow("u1")
	rl.Forget("u1")
	if !rl.Allow("u1") {
		t.Errorf("after Forget, bucket should be reset")
	}
}
