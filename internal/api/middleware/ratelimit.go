package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type rateBucket struct {
	windowStart time.Time
	count       int
}

type RateLimiter struct {
	limit  int
	window time.Duration

	mu      sync.Mutex
	buckets map[string]rateBucket
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		limit:   limit,
		window:  window,
		buckets: map[string]rateBucket{},
	}
}

func (r *RateLimiter) Allow(key string, now time.Time) bool {
	if r == nil || r.limit <= 0 || r.window <= 0 {
		return true
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	bucket := r.buckets[key]
	if bucket.windowStart.IsZero() || now.Sub(bucket.windowStart) >= r.window {
		bucket = rateBucket{
			windowStart: now,
			count:       0,
		}
	}

	if bucket.count >= r.limit {
		r.buckets[key] = bucket
		return false
	}

	bucket.count++
	r.buckets[key] = bucket

	if len(r.buckets) > 50000 {
		r.compactLocked(now)
	}

	return true
}

func (r *RateLimiter) compactLocked(now time.Time) {
	for key, bucket := range r.buckets {
		if now.Sub(bucket.windowStart) >= r.window {
			delete(r.buckets, key)
		}
	}
}

func RateLimit(limiter *RateLimiter, scope string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if limiter == nil {
			next.ServeHTTP(w, req)
			return
		}

		key := scope + ":" + clientKey(req)
		if !limiter.Allow(key, time.Now().UTC()) {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"type":"RATE_LIMITED"}`))
			return
		}

		next.ServeHTTP(w, req)
	})
}

func clientKey(req *http.Request) string {
	forwarded := strings.TrimSpace(req.Header.Get("X-Forwarded-For"))
	if forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			value := strings.TrimSpace(parts[0])
			if value != "" {
				return value
			}
		}
	}

	remote := strings.TrimSpace(req.RemoteAddr)
	if host, _, err := net.SplitHostPort(remote); err == nil && host != "" {
		return host
	}
	if remote != "" {
		return remote
	}

	return "unknown"
}
