package main

import (
	"encoding/json"
	"net"
	"net/http"
	"sync"

	"golang.org/x/time/rate"
)

// ── Per-IP rate limiter ───────────────────────────────────────────────────────

// ipLimiter maps a client IP address to its per-IP rate limiter.
type ipLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
}

func newIPLimiter() *ipLimiter {
	return &ipLimiter{limiters: make(map[string]*rate.Limiter)}
}

func (l *ipLimiter) get(ip string, r rate.Limit, b int) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	if lim, ok := l.limiters[ip]; ok {
		return lim
	}
	lim := rate.NewLimiter(r, b)
	l.limiters[ip] = lim
	return lim
}

var (
	generalLimiter = newIPLimiter() // 200 req/min
	uploadLimiter  = newIPLimiter() // 30 req/min
)

// rateLimit wraps an HTTP handler with per-IP rate limiting.
func rateLimit(next http.HandlerFunc, lim *ipLimiter, r rate.Limit, b int) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ip, _, _ := net.SplitHostPort(req.RemoteAddr)
		if !lim.get(ip, r, b).Allow() {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next(w, req)
	}
}

// ── JSON response helpers ─────────────────────────────────────────────────────

// writeJSON serialises v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON {"error": message} body with the given status code.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
