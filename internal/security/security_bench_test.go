package security

import "testing"

func BenchmarkHashCPF(b *testing.B) {
	cpf := "12345678901"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = HashCPF(cpf)
	}
}

func BenchmarkHashPasscode(b *testing.B) {
	code := "1234"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = HashPasscode(code)
	}
}

func BenchmarkGeneratePasscode(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = GeneratePasscode()
	}
}

func BenchmarkGenerateVoterToken(b *testing.B) {
	cpf := "98765432100"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = GenerateVoterToken(cpf)
	}
}

func BenchmarkValidateVoterToken(b *testing.B) {
	token := GenerateVoterToken("11144477735")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ValidateVoterToken(token)
	}
}

func BenchmarkGenerateJWT(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = GenerateJWT("admin", 1)
	}
}

func BenchmarkValidateJWT(b *testing.B) {
	token := GenerateJWT("admin", 1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = ValidateJWT(token)
	}
}

// HashPassword é intencionalmente caro (Argon2id). Útil para ver custo por login.
func BenchmarkHashPassword(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = HashPassword("SenhaForte!123")
	}
}
