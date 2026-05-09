package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIPRateLimitBurstAndThrottle(t *testing.T) {
	mw := IPRateLimit(3, 1)
	count := 0
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count++
		w.WriteHeader(http.StatusOK)
	}))
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "1.2.3.4:5000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("burst[%d] code=%d", i, rec.Code)
		}
	}
	// 4th request from same IP should 429.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:5000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}

	// A different IP still passes.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "9.9.9.9:5000"
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("other IP got %d", rec2.Code)
	}
	if count != 4 {
		t.Errorf("handler invoked %d times, want 4", count)
	}
}

func TestClientIPHonorsXForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.4, 10.0.0.1")
	if got := clientIP(req); got != "203.0.113.4" {
		t.Errorf("got %q, want 203.0.113.4", got)
	}
}
