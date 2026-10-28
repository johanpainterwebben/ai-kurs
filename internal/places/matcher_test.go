package places

import "testing"

func TestPrefixDistance(t *testing.T) {
	tests := []struct {
		q, c string
		want int
	}{
		{"upps", "uppsala", 0},
		{"upsa", "uppsala", 1},
		{"uppsla", "uppsala", 1},
		{"lnikoping", "linkoping", 1}, // byte av två tecken räknas som ett fel
		{"xyz", "uppsala", 3},
		{"abc", "a", 2},
	}
	for _, tt := range tests {
		m := newMatcher(tt.q)
		m.typos = 99 // räkna exakt, utan att avbryta tidigt
		if got := m.prefixDistance([]rune(tt.c)); got != tt.want {
			t.Errorf("prefixDistance(%q, %q) = %d, vill ha %d", tt.q, tt.c, got, tt.want)
		}
	}
}

func TestMatchScoreOrder(t *testing.T) {
	best := []matchScore{
		{class: classExact},
		{class: classPrefix},
		{class: classWord},
		{class: classContains},
		{class: classTypo, typos: 1},
		{class: classTypo, typos: 2},
	}
	for i := 1; i < len(best); i++ {
		if best[i-1].cmp(best[i]) >= 0 {
			t.Errorf("%+v ska rangas före %+v", best[i-1], best[i])
		}
	}
	for _, s := range best {
		if got, want := s.fuzzy(), s.class == classTypo; got != want {
			t.Errorf("%+v: fuzzy = %v, vill ha %v", s, got, want)
		}
		via := s
		via.viaOrt = true
		if s.cmp(via) >= 0 {
			t.Errorf("%+v ska rangas före samma träff via orten", s)
		}
	}
}

// En träff via orten delar rang med en träff med ett skrivfel; det är
// avsiktligt (se viaOrtPenalty) och ska inte ändras utan att man märker det.
func TestViaOrtRanksLikeOneTypo(t *testing.T) {
	via := matchScore{class: classPrefix, viaOrt: true}
	typo := matchScore{class: classTypo, typos: 1}
	if via.cmp(typo) != 0 {
		t.Errorf("weights: via ort %d, ett skrivfel %d", via.weight(), typo.weight())
	}
}

func TestMaxTypos(t *testing.T) {
	for n, want := range map[int]int{1: 0, 3: 0, 4: 1, 7: 1, 8: 2, 20: 2} {
		if got := maxTypos(n); got != want {
			t.Errorf("maxTypos(%d) = %d, vill ha %d", n, got, want)
		}
	}
}
