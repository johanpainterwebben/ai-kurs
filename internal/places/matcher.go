package places

import (
	"cmp"
	"strings"
)

// matchKey är ett vikt namn (med alias) förberett för matchning.
type matchKey struct {
	str        string
	runes      []rune
	wordStarts []int
}

func newMatchKey(s string) matchKey {
	str := withAliases(fold(s))
	k := matchKey{str: str, runes: []rune(str), wordStarts: []int{0}}
	for i, r := range k.runes {
		if r == ' ' {
			k.wordStarts = append(k.wordStarts, i+1)
		}
	}
	return k
}

// matchClass säger hur en fråga matchar ett namn, från bäst till sämst.
type matchClass uint8

const (
	classExact    matchClass = iota // platsens namn ("lund" för Lunds kommun)
	classPrefix                     // början av namnet
	classWord                       // början av ett ord i namnet
	classContains                   // någonstans i namnet
	classTypo                       // början av namnet eller ett ord, med skrivfel
)

// viaOrtPenalty är hur många steg sämre en plats rankas när frågan bara
// matchar via orten. En träff via orten ("lund" för "Lund/Tre Högars väg")
// hamnar därmed efter kommuner och orter, så att de inte trängs undan av alla
// platser i orten. Straffet gör också att den delar rang med en träff med ett
// skrivfel; det är avsiktligt, och kindOrder avgör sedan i rank.
const viaOrtPenalty = 3

// matchScore rangordnar hur väl en fråga matchar en plats; lägre är bättre.
type matchScore struct {
	class  matchClass
	typos  uint8 // antal skrivfel, för classTypo
	viaOrt bool  // frågan matchar bara via ortens namn
}

// fuzzy anger att träffen bygger på ett skrivfel.
func (s matchScore) fuzzy() bool { return s.class == classTypo }

// weight är rangen som ett tal: -1 för exakt, 0 för början av namnet, 1 för
// början av ett ord, 2 för någonstans i namnet och 2+n för n skrivfel. En
// träff via orten ligger viaOrtPenalty steg längre ned.
func (s matchScore) weight() int {
	var w int
	switch s.class {
	case classExact:
		w = -1
	case classTypo:
		w = int(classContains) - 1 + int(s.typos) // som "någonstans i namnet" plus ett steg per skrivfel
	default:
		w = int(s.class) - 1
	}
	if s.viaOrt {
		w += viaOrtPenalty
	}
	return w
}

func (s matchScore) cmp(o matchScore) int { return cmp.Compare(s.weight(), o.weight()) }

// matcher jämför en fråga mot många platser. Den skapas en gång per
// sökning och återanvänder sina buffertar, så att en jämförelse inte
// allokerar minne – förslagen anropas vid varje tangenttryckning.
type matcher struct {
	q     []rune
	qs    string // q som sträng
	qWord string // " " + q, för träff i början av ett ord
	typos int
	rows  [3][]int // de tre senaste raderna i prefixDistance
}

func newMatcher(q string) *matcher {
	m := &matcher{q: []rune(q), qs: q, qWord: " " + q}
	m.typos = maxTypos(len(m.q))
	for i := range m.rows {
		m.rows[i] = make([]int, len(m.q)+2)
	}
	return m
}

// match jämför frågan med platsens namn. En plats inom en ort ("Lund/Tre
// Högars väg") matchas i första hand på sitt eget namn; matchar frågan bara
// via orten får träffen viaOrt.
func (m *matcher) match(p *place) (matchScore, bool) {
	if p.Kind == KindPlats {
		if s, ok := m.scoreKey(p.own); ok {
			return s, true
		}
		if s, ok := m.scoreKey(p.full); ok {
			s.viaOrt = true
			return s, true
		}
		return matchScore{}, false
	}
	return m.scoreKey(p.full)
}

// maxTypos är hur många skrivfel som tillåts för en fråga av given längd.
func maxTypos(n int) int {
	switch {
	case n < 4:
		return 0
	case n < 8:
		return 1
	default:
		return 2
	}
}

// scoreKey rangordnar hur väl frågan matchar k.
func (m *matcher) scoreKey(k matchKey) (matchScore, bool) {
	switch {
	case strings.HasPrefix(k.str, m.qs):
		return matchScore{class: classPrefix}, true
	case strings.Contains(k.str, m.qWord):
		return matchScore{class: classWord}, true
	case strings.Contains(k.str, m.qs):
		return matchScore{class: classContains}, true
	case m.typos == 0:
		return matchScore{}, false
	}
	best := m.typos + 1
	for _, start := range k.wordStarts {
		best = min(best, m.prefixDistance(k.runes[start:]))
		if best == 1 {
			break // bättre än ett fel går inte, utan exakt träff ovan
		}
	}
	if best > m.typos {
		return matchScore{}, false
	}
	return matchScore{class: classTypo, typos: uint8(best)}, true
}
