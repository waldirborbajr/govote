package web

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

const (
	csrfCookieName = "csrf_token"
	csrfHeaderName = "X-CSRF-Token"
	csrfFormField  = "csrf_token"
	csrfTokenBytes = 32
)

// SecurityHeadersMiddleware sets defensive HTTP response headers on every response.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}

// EnsureCSRFToken guarantees a CSRF cookie exists and returns its value.
func EnsureCSRFToken(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(csrfCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	token := randomCSRFToken(csrfTokenBytes)
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: false,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	return token
}

// CheckCSRF validates the CSRF token for state-changing requests.
func CheckCSRF(r *http.Request) bool {
	c, err := r.Cookie(csrfCookieName)
	if err != nil || c.Value == "" {
		return false
	}
	provided := r.Header.Get(csrfHeaderName)
	if provided == "" {
		_ = r.ParseForm()
		provided = r.FormValue(csrfFormField)
	}
	if provided == "" {
		return false
	}
	return subtleConstantTimeEq(provided, c.Value)
}

func subtleConstantTimeEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func randomCSRFToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString(make([]byte, n))
	}
	return hex.EncodeToString(b)
}

// CSRFMiddleware rejects mutating /ui/ requests without a valid CSRF token.
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		EnsureCSRFToken(w, r)

		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		if strings.HasPrefix(r.URL.Path, "/ui/") {
			if !CheckCSRF(r) {
				RespondError(w, http.StatusForbidden, "csrf token inválido ou ausente")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Chain applies middlewares in order (first is outermost).
func Chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
