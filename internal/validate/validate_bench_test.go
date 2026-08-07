package validate

import "testing"

func BenchmarkCPF(b *testing.B) {
	// CPF sintético — o algoritmo de dígitos pode falhar; medimos o caminho completo.
	cpf := "52998224725"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = CPF(cpf)
	}
}

func BenchmarkCPFInvalid(b *testing.B) {
	cpf := "11111111111"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = CPF(cpf)
	}
}

func BenchmarkName(b *testing.B) {
	name := "Maria da Silva Santos"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Name(name)
	}
}

func BenchmarkPhone(b *testing.B) {
	phone := "+5511999998888"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Phone(phone)
	}
}

func BenchmarkPollTitle(b *testing.B) {
	title := "Black Friday 2026 - Qual produto você mais quer?"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = PollTitle(title)
	}
}

func BenchmarkRFC3339Date(b *testing.B) {
	s := "2026-11-28T00:00:00Z"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = RFC3339Date(s, "start_date")
	}
}

func BenchmarkAnswerTexts(b *testing.B) {
	answers := []string{"Smartphone", "Notebook", "TV 4K", "Fone", "Console"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = AnswerTexts(answers)
	}
}
