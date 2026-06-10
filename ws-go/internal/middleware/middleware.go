package middleware

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"ws-go/internal/metrics"
)

// CORSMiddleware adds CORS headers to allow cross-origin requests.
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}

		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
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

// RateLimiter provides simple per-IP rate limiting.
type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     int
	window   time.Duration
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

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	for ip, v := range rl.visitors {
		if time.Since(v.lastSeen) > rl.window {
			delete(rl.visitors, ip)
		}
	}
}

// KeyRateLimiter provides per-API-key rate limiting.
type KeyRateLimiter struct {
	mu     sync.Mutex
	keys   map[string]*visitor
	rate   int
	window time.Duration
}

// NewKeyRateLimiter creates a per-key rate limiter.
func NewKeyRateLimiter(rate int, window time.Duration) *KeyRateLimiter {
	krl := &KeyRateLimiter{
		keys:   make(map[string]*visitor),
		rate:   rate,
		window: window,
	}

	go func() {
		for {
			time.Sleep(time.Minute)
			krl.cleanup()
		}
	}()

	return krl
}

// Allow checks if the key is within its rate limit.
func (krl *KeyRateLimiter) Allow(key string) bool {
	krl.mu.Lock()
	defer krl.mu.Unlock()

	v, exists := krl.keys[key]
	if !exists || time.Since(v.lastSeen) > krl.window {
		krl.keys[key] = &visitor{count: 1, lastSeen: time.Now()}
		return true
	}

	v.count++
	v.lastSeen = time.Now()
	return v.count <= krl.rate
}

func (krl *KeyRateLimiter) cleanup() {
	krl.mu.Lock()
	defer krl.mu.Unlock()

	for key, v := range krl.keys {
		if time.Since(v.lastSeen) > krl.window {
			delete(krl.keys, key)
		}
	}
}

// RateLimitMiddleware applies rate limiting to HTTP requests.
func RateLimitMiddleware(rl *RateLimiter, m *metrics.Metrics, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			ip = strings.Split(forwarded, ",")[0]
		}

		if !rl.Allow(strings.TrimSpace(ip)) {
			log.Printf("[rate-limit] blocked IP: %s on %s", ip, r.URL.Path)
			if m != nil {
				m.RateLimitHits.Add(1)
			}
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
