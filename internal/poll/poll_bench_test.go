package poll

import "testing"

func BenchmarkIsActive(b *testing.B) {
	start := "2025-01-01T00:00:00Z"
	end := "2027-12-31T23:59:59Z"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = IsActive(start, end)
	}
}

func BenchmarkIsActiveExpired(b *testing.B) {
	start := "2020-01-01T00:00:00Z"
	end := "2020-12-31T23:59:59Z"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = IsActive(start, end)
	}
}
