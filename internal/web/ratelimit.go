package web

import (
	"net/http"
	"os"
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

// trustProxyHeaders controla se X-Forwarded-For/X-Real-IP são usados para
// identificar o IP do cliente. Esses headers podem ser forjados por qualquer
// requisição direta, então só devem ser confiados quando a aplicação roda
// atrás de um proxy reverso confiável (nginx, Cloudflare etc.) que os
// sobrescreve. Sem isso, o rate limiter pode ser burlado trivialmente
// enviando um X-Forwarded-For diferente a cada requisição.
var trustProxyHeaders = os.Getenv("GOVOTE_TRUST_PROXY_HEADERS") == "true"

func getClientIP(r *http.Request) string {
	if trustProxyHeaders {
		if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
			// X-Forwarded-For pode ser uma lista "client, proxy1, proxy2";
			// o primeiro IP é o do cliente original.
			if idx := strings.Index(ip, ","); idx != -1 {
				ip = ip[:idx]
			}
			return strings.TrimSpace(stripPort(ip))
		}
		if ip := r.Header.Get("X-Real-IP"); ip != "" {
			return stripPort(ip)
		}
	}
	return stripPort(r.RemoteAddr)
}

func stripPort(ip string) string {
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
