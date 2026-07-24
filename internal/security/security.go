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
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// jwtSecret assina os tokens de sessão de admin. DEVE ser definido via a
// variável de ambiente GOVOTE_JWT_SECRET em qualquer ambiente que não seja
// desenvolvimento local — o valor de fallback é público (está no código-fonte)
// e permite forjar tokens de admin.
var jwtSecret = loadSecret("GOVOTE_JWT_SECRET", "insecure-dev-jwt-secret-change-me")

// cpfPepper é o "salt" fixo (por deployment) usado em HashCPF. DEVE ser
// definido via GOVOTE_CPF_PEPPER em produção — ver comentário de HashCPF
// para o porquê de não ser um salt aleatório por registro.
var cpfPepper = loadSecret("GOVOTE_CPF_PEPPER", "insecure-dev-cpf-pepper-change-me")

// loadSecret lê um segredo de variável de ambiente, ou usa um valor de
// desenvolvimento (registrando um aviso) caso não esteja definida.
func loadSecret(envVar, devFallback string) []byte {
	if v := os.Getenv(envVar); v != "" {
		return []byte(v)
	}
	log.Printf("⚠️  %s não definida — usando valor de desenvolvimento INSEGURO. Configure essa variável antes de ir para produção.", envVar)
	return []byte(devFallback)
}

// Argon2id parameters para senhas de admin/voter (operação pouco frequente,
// pode ser mais custosa).
const (
	argonMemory  = 65536 // memory_cost in KiB (64 MB)
	argonTime    = 7     // time_cost (iterations)
	argonThreads = 8     // threads (parallelism)
	argonKeyLen  = 32
	argonSaltLen = 16
)

// Argon2id parameters para hashing de CPF. HashCPF roda em praticamente toda
// requisição relevante (votar, verificar, listar resultados por admin), então
// os custos são calibrados mais baixo que os de senha — ainda assim, no
// mínimo recomendado pela OWASP (m=19MB, t=2, p=1), o que já é ordens de
// magnitude mais caro que o SHA-256 de passagem única usado antes.
const (
	cpfArgonMemory  = 19 * 1024 // 19 MB
	cpfArgonTime    = 2
	cpfArgonThreads = 1
	cpfArgonKeyLen  = 32
)

// HashCPF retorna um hash Argon2id determinístico do CPF, usado como chave
// de busca/unicidade (voter_hash) na tabela votes.
//
// Diferente de HashPassword, este hash NÃO usa salt aleatório por registro:
// o sistema precisa reencontrar o mesmo voter_hash a partir do mesmo CPF
// (checagem de "já votou" via UNIQUE(poll_id, voter_hash) e WHERE cpf = ?),
// então a função tem que ser pura em relação à entrada. Em vez de salt
// aleatório, usamos um pepper fixo por deployment (cpfPepper) como salt do
// Argon2id.
//
// Isso não torna o CPF "impossível" de recuperar (CPF só tem ~10^11 valores
// possíveis, então um espaço de busca pequeno e estruturado sempre é
// atacável por força bruta se o pepper vazar junto com o banco). O ganho
// real é que cada tentativa agora custa uma execução completa de Argon2id
// (memory-hard) em vez de um único SHA-256 — isso torna a varredura de todo
// o espaço de CPFs várias ordens de magnitude mais lenta. cpfPepper precisa
// ser tratado com o mesmo cuidado que uma chave de banco de dados: se vazar
// junto com os dados, essa proteção deixa de valer.
func HashCPF(cpf string) string {
	// Argon2id exige um salt; derivamos um de 16 bytes fixo a partir do
	// pepper para manter o hash determinístico entre chamadas/reinícios.
	saltSum := sha256.Sum256(cpfPepper)
	salt := saltSum[:16]

	hash := argon2.IDKey([]byte(cpf), salt, cpfArgonTime, cpfArgonMemory, cpfArgonThreads, cpfArgonKeyLen)
	return hex.EncodeToString(hash)
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

// GenerateTemporaryPassword generates a strong temporary password:
// 8 random digits + 1 special character (for first-use admin access).
func GenerateTemporaryPassword() string {
	// 8 digits
	digits := make([]byte, 8)
	_, err := rand.Read(digits)
	if err != nil {
		log.Fatalf("rand failed: %v", err)
	}
	for i := range digits {
		digits[i] = byte('0' + (digits[i] % 10))
	}

	// One special char
	specials := []byte{'!', '@', '#', '$', '%', '&', '*', '?'}
	special := specials[int(digits[0])%len(specials)]

	return string(digits) + string(special)
}

// GeneratePasscode keeps the old 4-digit for voters (backward compatibility).
func GeneratePasscode() string {
	b := make([]byte, 2)
	_, err := rand.Read(b)
	if err != nil {
		log.Fatalf("rand failed: %v", err)
	}
	val := (int(b[0])<<8 | int(b[1])) % 10000
	return fmt.Sprintf("%04d", val)
}

// Argon2id parameters para passcodes (OTP). Diferente de HashCPF, o passcode
// é comparado 1:1 (não é usado como chave de busca), então usa salt aleatório
// por registro como uma senha normal. Os parâmetros são mais leves que
// argonMemory/argonTime porque HashPasscode roda no caminho crítico de todo
// pedido/verificação de OTP (voter e admin), mas ainda assim são muito mais
// caros que a comparação em texto puro que existia antes.
const (
	passcodeArgonMemory  = 8 * 1024 // 8 MB
	passcodeArgonTime    = 1
	passcodeArgonThreads = 1
)

// HashPasscode hashes a short numeric OTP passcode using Argon2id, encoded in
// the same PHC format as HashPassword, so it never needs to be stored or
// compared in plain text.
func HashPasscode(passcode string) string {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		log.Fatalf("rand failed: %v", err)
	}

	hash := argon2.IDKey([]byte(passcode), salt, passcodeArgonTime, passcodeArgonMemory, passcodeArgonThreads, argonKeyLen)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, passcodeArgonMemory, passcodeArgonTime, passcodeArgonThreads, b64Salt, b64Hash,
	)
}

// CheckPasscode reports whether passcode matches a hash produced by
// HashPasscode. It delegates to CheckPassword: that routine reads its
// Argon2id parameters from the stored hash itself, so it verifies any
// PHC-format hash regardless of which Hash* function created it.
func CheckPasscode(storedHash, passcode string) bool {
	return CheckPassword(storedHash, passcode)
}
