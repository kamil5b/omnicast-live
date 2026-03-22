package main

import (
	"net"
	"net/http"
	"sync"
	"time"

	"encoding/json"

	"golang.org/x/time/rate"
)

// ── Per-IP rate limiter ───────────────────────────────────────────────────────

const ipLimiterTTL = 5 * time.Minute

// ipEntry pairs a rate limiter with the last time it was accessed.
type ipEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// ipLimiter maps a client IP address to its per-IP rate limiter.
// Entries that have not been accessed within ipLimiterTTL are evicted
// periodically to prevent unbounded memory growth.
type ipLimiter struct {
	mu      sync.Mutex
	entries map[string]*ipEntry
}

func newIPLimiter() *ipLimiter {
	l := &ipLimiter{entries: make(map[string]*ipEntry)}
	go l.evictLoop()
	return l
}

// get returns the rate limiter for ip, creating one if needed.
// r and b are only used when creating a new limiter; existing limiters keep
// their original parameters.
func (l *ipLimiter) get(ip string, r rate.Limit, b int) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e, ok := l.entries[ip]; ok {
		e.lastSeen = time.Now()
		return e.limiter
	}
	lim := rate.NewLimiter(r, b)
	l.entries[ip] = &ipEntry{limiter: lim, lastSeen: time.Now()}
	return lim
}

// evictLoop runs in the background and removes stale entries every TTL period.
func (l *ipLimiter) evictLoop() {
	ticker := time.NewTicker(ipLimiterTTL)
	defer ticker.Stop()
	for range ticker.C {
		l.evict()
	}
}

func (l *ipLimiter) evict() {
	cutoff := time.Now().Add(-ipLimiterTTL)
	l.mu.Lock()
	defer l.mu.Unlock()
	for ip, e := range l.entries {
		if e.lastSeen.Before(cutoff) {
			delete(l.entries, ip)
		}
	}
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
