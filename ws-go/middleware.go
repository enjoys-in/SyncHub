package main

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// CORSMiddleware adds CORS headers to allow cross-origin WebSocket connections.
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}

		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-User-Token, X-API-Key")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// AuthMiddleware extracts the user token from the request.
// Supports: query param (?token=xxx), Authorization header, or X-User-Token header.
func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			http.Error(w, `{"error":"authentication required — provide token via query param, Authorization header, or X-User-Token header"}`, http.StatusUnauthorized)
			return
		}

		// For now, the token IS the user ID (simple mode).
		// In production, you'd validate a JWT here and extract the user ID from claims.
		// Example JWT validation:
		//   claims, err := jwt.Validate(token)
		//   userID = claims.Subject

		next.ServeHTTP(w, r)
	}
}

// extractToken pulls the auth token from the request (query param > header > bearer).
func extractToken(r *http.Request) string {
	// 1. Query parameter (most common for WebSocket)
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}

	// 2. X-User-Token header
	if token := r.Header.Get("X-User-Token"); token != "" {
		return token
	}

	// 3. Authorization: Bearer <token>
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}

	return ""
}

// RateLimiter provides simple per-IP rate limiting.
type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     int           // Max requests per window
	window   time.Duration // Time window
}

type visitor struct {
	count    int
	lastSeen time.Time
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(rate int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate,
		window:   window,
	}

	// Cleanup stale entries every minute
	go func() {
		for {
			time.Sleep(time.Minute)
			rl.cleanup()
		}
	}()

	return rl
}

// Allow checks if the IP is within the rate limit.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	if !exists || time.Since(v.lastSeen) > rl.window {
		rl.visitors[ip] = &visitor{count: 1, lastSeen: time.Now()}
		return true
	}

	v.count++
	v.lastSeen = time.Now()
	return v.count <= rl.rate
}

// cleanup removes stale entries.
func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	for ip, v := range rl.visitors {
		if time.Since(v.lastSeen) > rl.window {
			delete(rl.visitors, ip)
		}
	}
}

// RateLimitMiddleware applies rate limiting to HTTP requests.
func RateLimitMiddleware(rl *RateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			ip = strings.Split(forwarded, ",")[0]
		}

		if !rl.Allow(strings.TrimSpace(ip)) {
			log.Printf("[rate-limit] blocked IP: %s", ip)
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	}
}

// LoggingMiddleware logs incoming HTTP requests.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("[http] %s %s %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
		log.Printf("[http] %s %s completed in %v", r.Method, r.URL.Path, time.Since(start))
	})
}
