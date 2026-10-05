package places

// Förslagen anropas vid varje tangenttryckning och kan anropas av vem som
// helst, så de ska vara billiga även för långa påhittade frågor (DoS).
// Kör med: go test -bench Suggest ./internal/places/

import (
	"strings"
	"testing"
)

func BenchmarkSuggestTypical(b *testing.B) {
	s := realSnapshot(b)
	b.ReportAllocs()
	for b.Loop() {
		s.Suggest("upsala", 8)
	}
}

// Värsta fallet: lång påhittad text som inte matchar något direkt, så att
// felstavningsjämförelsen körs mot alla platser.
func BenchmarkSuggestWorstCase(b *testing.B) {
	s := realSnapshot(b)
	q := strings.Repeat("qxzv", 12) + "qx"
	b.ReportAllocs()
	for b.Loop() {
		s.Suggest(q, 8)
	}
}
