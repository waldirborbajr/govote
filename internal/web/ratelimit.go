package web

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter keeps per-IP request timestamps for a sliding-window limiter.
type RateLimiter struct {
	visits sync.Map // IP -> []time.Time
}

var rateLimiter = &RateLimiter{}

const (
	maxRequestsPerMinute = 10
	windowDuration       = 60 * time.Second
)

func isRateLimited(ip string) bool {
	now := time.Now()
	var times []time.Time

	if val, ok := rateLimiter.visits.Load(ip); ok {
		times = val.([]time.Time)
	}

	var validTimes []time.Time
	for _, t := range times {
		if now.Sub(t) < windowDuration {
			validTimes = append(validTimes, t)
		}
	}

	if len(validTimes) >= maxRequestsPerMinute {
		rateLimiter.visits.Store(ip, validTimes)
		return true
	}

	validTimes = append(validTimes, now)
	rateLimiter.visits.Store(ip, validTimes)
	return false
}

func getClientIP(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.Header.Get("X-Real-IP")
	}
	if ip == "" {
		ip = r.RemoteAddr
	}
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

// RateLimitMiddleware rejects requests from an IP that exceeds the per-minute
// request budget.
func RateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r)
		if isRateLimited(ip) {
			RespondError(w, http.StatusTooManyRequests, "Muitas requisições. Aguarde um momento.")
			return
		}
		next.ServeHTTP(w, r)
	}
}
