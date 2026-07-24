package httpapi

import (
	"net/http"
	"sync"
	"time"
)

// rateLimiter is a simple per-key token bucket. Keys are actor IDs, so each API
// key (and the trust-all local actor) gets its own budget. It guards against
// runaway agents (Phase 22 risk) without external dependencies.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	rate    float64 // tokens refilled per second
	burst   float64 // bucket capacity
}

type tokenBucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(rate, burst float64) *rateLimiter {
	if rate <= 0 {
		rate = 50
	}
	if burst <= 0 {
		burst = rate * 4
	}
	return &rateLimiter{buckets: map[string]*tokenBucket{}, rate: rate, burst: burst}
}

// allow consumes one token for key, returning false when the bucket is empty.
func (l *rateLimiter) allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil {
		b = &tokenBucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * l.rate
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// rateLimit middleware enforces a general per-actor request rate plus a stricter
// per-actor write budget. It runs after authMiddleware so the actor is known.
func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only rate-limit the API surface; static SPA assets are unrestricted.
		if len(r.URL.Path) < 5 || r.URL.Path[:5] != "/api/" || r.URL.Path == "/api/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		actor := actorFrom(r.Context())
		if !s.reqLimiter.allow(actor.ID) {
			writeErrorStatus(w, http.StatusTooManyRequests, "rate.limited", "request rate limit exceeded")
			return
		}
		if isWriteMethod(r.Method) && !s.writeLimiter.allow(actor.ID) {
			writeErrorStatus(w, http.StatusTooManyRequests, "rate.write_limited", "write rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
