// Package web provides HTTP helpers: responses, static assets, session
// resolution and rate limiting.
package web

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Rate limiter — sliding window por IP (stdlib only)
//
// Limites padrão (sobrescrevíveis por env):
//   GOVOTE_RATE_LIMIT_MAX=10          // pedidos por janela
//   GOVOTE_RATE_LIMIT_WINDOW_SEC=60   // tamanho da janela em segundos
//
// Proxy:
//   GOVOTE_TRUST_PROXY_HEADERS=true   // só atrás de proxy confiável
// ---------------------------------------------------------------------------

const (
	defaultMaxRequests = 10
	defaultWindowSec   = 60
	cleanupInterval    = 5 * time.Minute
)

// RateLimiter keeps per-IP request timestamps for a sliding-window limiter.
type RateLimiter struct {
	mu     sync.Mutex
	visits map[string][]time.Time
	max    int
	window time.Duration
}

var rateLimiter = newRateLimiter()

func newRateLimiter() *RateLimiter {
	max := defaultMaxRequests
	if v := os.Getenv("GOVOTE_RATE_LIMIT_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			max = n
		}
	}

	windowSec := defaultWindowSec
	if v := os.Getenv("GOVOTE_RATE_LIMIT_WINDOW_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			windowSec = n
		}
	}

	rl := &RateLimiter{
		visits: make(map[string][]time.Time),
		max:    max,
		window: time.Duration(windowSec) * time.Second,
	}

	// Evita crescimento ilimitado do mapa em processos de longa duração.
	go rl.cleanupLoop()

	return rl
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		rl.cleanup()
	}
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for ip, times := range rl.visits {
		valid := times[:0]
		for _, t := range times {
			if now.Sub(t) < rl.window {
				valid = append(valid, t)
			}
		}
		if len(valid) == 0 {
			delete(rl.visits, ip)
		} else {
			rl.visits[ip] = valid
		}
	}
}

// allow registra a requisição e retorna (ok, retryAfter).
// ok=false significa que o IP excedeu o limite.
func (rl *RateLimiter) allow(ip string) (ok bool, retryAfter time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	times := rl.visits[ip]

	// Descarta timestamps fora da janela.
	valid := times[:0]
	for _, t := range times {
		if now.Sub(t) < rl.window {
			valid = append(valid, t)
		}
	}

	if len(valid) >= rl.max {
		rl.visits[ip] = valid
		// Tempo até o pedido mais antigo sair da janela.
		oldest := valid[0]
		retryAfter = rl.window - now.Sub(oldest)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		return false, retryAfter
	}

	valid = append(valid, now)
	rl.visits[ip] = valid
	return true, 0
}

// trustProxyHeaders controla se X-Forwarded-For / X-Real-IP são usados para
// identificar o IP do cliente. Esses headers podem ser forjados por qualquer
// requisição direta, então só devem ser confiados quando a aplicação roda
// atrás de um proxy reverso confiável (nginx, Caddy, Cloudflare etc.) que os
// sobrescreve. Sem isso, o rate limiter pode ser burlado trivialmente
// enviando um X-Forwarded-For diferente a cada requisição.
var trustProxyHeaders = os.Getenv("GOVOTE_TRUST_PROXY_HEADERS") == "true"

func getClientIP(r *http.Request) string {
	if trustProxyHeaders {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// Lista "client, proxy1, proxy2" — o primeiro é o cliente original.
			if idx := strings.Index(xff, ","); idx != -1 {
				xff = xff[:idx]
			}
			if ip := normalizeIP(strings.TrimSpace(xff)); ip != "" {
				return ip
			}
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			if ip := normalizeIP(strings.TrimSpace(xri)); ip != "" {
				return ip
			}
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr sem porta (raro) ou já só o IP.
		return normalizeIP(r.RemoteAddr)
	}
	return normalizeIP(host)
}

// normalizeIP valida e canônica o endereço (IPv4 / IPv6).
func normalizeIP(s string) string {
	s = strings.TrimSpace(s)
	// Remove colchetes de IPv6 literal: "[::1]"
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	ip := net.ParseIP(s)
	if ip == nil {
		return s // fallback: string original (não deveria acontecer)
	}
	return ip.String()
}

// RateLimitMiddleware rejects requests from an IP that exceeds the per-window
// request budget. Responde 429 com Retry-After quando bloqueado.
func RateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r)
		ok, retryAfter := rateLimiter.allow(ip)
		if !ok {
			secs := int(retryAfter.Seconds())
			if secs < 1 {
				secs = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			RespondError(w, http.StatusTooManyRequests, "Muitas requisições. Aguarde um momento.")
			return
		}
		next.ServeHTTP(w, r)
	}
}
