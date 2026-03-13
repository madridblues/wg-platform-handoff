package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterAllowWindow(t *testing.T) {
	limiter := NewRateLimiter(2, time.Minute)
	now := time.Now().UTC()

	if !limiter.Allow("k", now) {
		t.Fatalf("expected first request to pass")
	}
	if !limiter.Allow("k", now) {
		t.Fatalf("expected second request to pass")
	}
	if limiter.Allow("k", now) {
		t.Fatalf("expected third request to be blocked")
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	limiter := NewRateLimiter(1, time.Minute)
	handler := RateLimit(limiter, "scope", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest(http.MethodPost, "/auth/v1/token", nil)
	req1.RemoteAddr = "198.51.100.10:12345"
	res1 := httptest.NewRecorder()
	handler.ServeHTTP(res1, req1)
	if res1.Code != http.StatusOK {
		t.Fatalf("expected first status 200, got %d", res1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/auth/v1/token", nil)
	req2.RemoteAddr = "198.51.100.10:12345"
	res2 := httptest.NewRecorder()
	handler.ServeHTTP(res2, req2)
	if res2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second status 429, got %d", res2.Code)
	}
}
