package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Auth provides token-based authentication middleware.
type Auth struct {
	token     string
	rateLimit *rateLimiter
}

// NewAuth creates a new auth middleware.
func NewAuth(token string) *Auth {
	return &Auth{
		token:     token,
		rateLimit: newRateLimiter(5, time.Minute),
	}
}

// Wrap returns an http.Handler that requires valid authentication.
func (a *Auth) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := extractIP(r)

		if a.rateLimit.isBlocked(ip) {
			http.Error(w, `{"error":"too many failed attempts"}`, http.StatusTooManyRequests)
			return
		}

		token := extractToken(r)
		if token == "" {
			a.rateLimit.record(ip)
			http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
			return
		}

		if subtle.ConstantTimeCompare([]byte(token), []byte(a.token)) != 1 {
			a.rateLimit.record(ip)
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}

		a.rateLimit.reset(ip)
		next.ServeHTTP(w, r)
	})
}

func extractToken(r *http.Request) string {
	// Check Authorization header
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}

	// Check query parameter (for WebSocket/SSE)
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}

	return ""
}

func extractIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.SplitN(fwd, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	host, _, _ := strings.Cut(r.RemoteAddr, ":")
	return host
}

// rateLimiter tracks failed auth attempts per IP.
type rateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	max      int
	window   time.Duration
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		attempts: make(map[string][]time.Time),
		max:      max,
		window:   window,
	}
}

func (rl *rateLimiter) record(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.attempts[ip] = append(rl.attempts[ip], time.Now())
}

func (rl *rateLimiter) isBlocked(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	attempts := rl.attempts[ip]
	cutoff := time.Now().Add(-rl.window)

	// Prune old entries
	valid := attempts[:0]
	for _, t := range attempts {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	rl.attempts[ip] = valid

	return len(valid) >= rl.max
}

func (rl *rateLimiter) reset(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.attempts, ip)
}
