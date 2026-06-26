// Package security holds the cryptographic helpers used for authentication:
// CPF/password hashing, signed-token (pseudo-JWT) generation/validation and
// random passcode generation.
package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

var jwtSecret = []byte("super-secret-jwt-key-change-in-production-2026")

const hashSalt = "super-secret-salt-value"

// HashCPF returns a deterministic, salted SHA-256 hash of a CPF so that voter
// identity can be stored without keeping the raw document number.
func HashCPF(cpf string) string {
	h := sha256.New()
	h.Write([]byte(cpf + hashSalt))
	return hex.EncodeToString(h.Sum(nil))
}

// HashPassword returns a salted SHA-256 hash (salt prepended) for a password.
func HashPassword(password string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	salted := append(salt, []byte(password)...)
	hash := sha256.Sum256(salted)
	return hex.EncodeToString(append(salt, hash[:]...))
}

// CheckPassword reports whether password matches a hash produced by HashPassword.
func CheckPassword(storedHash, password string) bool {
	raw, err := hex.DecodeString(storedHash)
	if err != nil || len(raw) < 16+sha256.Size {
		return false
	}
	salt := raw[:16]
	expectedHash := raw[16:]

	h := sha256.New()
	h.Write(salt)
	h.Write([]byte(password))
	return hex.EncodeToString(h.Sum(nil)) == hex.EncodeToString(expectedHash)
}

// GenerateJWT cria um token assinado no formato username|expiry|hmac(username|expiry).
// Não é um JWT padrão (RFC 7519), mas é assinado com HMAC-SHA256, então não pode
// ser forjado sem conhecer jwtSecret.
func GenerateJWT(username string) string {
	expiry := time.Now().Add(24 * time.Hour).Unix()
	payload := fmt.Sprintf("%s|%d", username, expiry)
	sig := signPayload(payload)
	return payload + "|" + sig
}

func signPayload(payload string) string {
	mac := hmac.New(sha256.New, jwtSecret)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// ValidateJWT verifies the signature and expiry of a token and returns the
// embedded username when valid.
func ValidateJWT(token string) (string, bool) {
	parts := strings.SplitN(token, "|", 3)
	if len(parts) != 3 {
		return "", false
	}
	username := parts[0]
	expiryStr := parts[1]
	sig := parts[2]

	payload := username + "|" + expiryStr
	expectedSig := signPayload(payload)
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return "", false
	}

	expiry, err := strconv.ParseInt(expiryStr, 10, 64)
	if err != nil || time.Now().Unix() > expiry {
		return "", false
	}
	return username, true
}

// GeneratePasscode returns a random 4-digit passcode.
func GeneratePasscode() string {
	b := make([]byte, 2)
	_, err := rand.Read(b)
	if err != nil {
		log.Fatalf("rand failed: %v", err)
	}
	val := (int(b[0])<<8 | int(b[1])) % 10000
	return fmt.Sprintf("%04d", val)
}
