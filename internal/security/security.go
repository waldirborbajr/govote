// Package security holds the cryptographic helpers used for authentication:
// CPF/password hashing, signed-token (pseudo-JWT) generation/validation and
// random passcode generation.
package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

var jwtSecret = []byte("super-secret-jwt-key-change-in-production-2026")
const hashSalt = "super-secret-salt-value"

// Argon2id parameters.
const (
	argonMemory  = 65536 // memory_cost in KiB (64 MB)
	argonTime    = 7     // time_cost (iterations)
	argonThreads = 8     // threads (parallelism)
	argonKeyLen  = 32
	argonSaltLen = 16
)

// HashCPF returns a deterministic, salted SHA-256 hash of a CPF so that voter
// identity can be stored without keeping the raw document number.
func HashCPF(cpf string) string {
	h := sha256.New()
	h.Write([]byte(cpf + hashSalt))
	return hex.EncodeToString(h.Sum(nil))
}

// HashPassword returns an Argon2id hash of password, encoded in the standard
// PHC string format: $argon2id$v=19$m=...,t=...,p=...$salt$hash
func HashPassword(password string) string {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		log.Fatalf("rand failed: %v", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64Salt, b64Hash,
	)
}

// CheckPassword reports whether password matches a hash produced by HashPassword.
// Parameters are read from the stored hash itself, so it stays compatible even
// if argonMemory/argonTime/argonThreads change later.
func CheckPassword(storedHash, password string) bool {
	parts := strings.Split(storedHash, "$")
	// parts: ["", "argon2id", "v=19", "m=...,t=...,p=...", "salt", "hash"]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false
	}
	if version != argon2.Version {
		return false
	}

	var mem uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &p); err != nil {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}

	computedHash := argon2.IDKey([]byte(password), salt, t, mem, p, uint32(len(expectedHash)))

	return subtle.ConstantTimeCompare(computedHash, expectedHash) == 1
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
