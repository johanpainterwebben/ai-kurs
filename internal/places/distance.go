package places

import "math"

// prefixDistance är det minsta redigeringsavståndet (med byte av två
// intilliggande tecken som ett fel) mellan frågan och någon början av c vars
// längd skiljer sig högst ett tecken från frågan. "upsa" mot "uppsala" ger 1.
//
// Räknas rad för rad med tre återanvända rader. Radernas minsta värde kan
// aldrig minska, så när det överstiger tillåtna skrivfel avbryts jämförelsen
// – det gör att påhittade frågor blir billiga. Svaret är då bara garanterat
// större än m.typos, inte exakt.
func (m *matcher) prefixDistance(c []rune) int {
	q := m.q
	c = c[:min(len(c), len(q)+1)]
	n := len(c)
	prev2, prev, cur := m.rows[0][:n+1], m.rows[1][:n+1], m.rows[2][:n+1]
	for j := range prev {
		prev[j] = j // avstånd mellan tom fråga och c[:j]
	}
	for i := 1; i <= len(q); i++ {
		cur[0] = i
		rowMin := i
		for j := 1; j <= n; j++ {
			cost := 1
			if q[i-1] == c[j-1] {
				cost = 0
			}
			v := min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && q[i-1] == c[j-2] && q[i-2] == c[j-1] {
				v = min(v, prev2[j-2]+1)
			}
			cur[j] = v
			rowMin = min(rowMin, v)
		}
		if rowMin > m.typos {
			return rowMin
		}
		prev2, prev, cur = prev, cur, prev2
	}
	// prev är nu sista raden: avståndet från hela frågan till c[:j].
	best := math.MaxInt
	for j := max(0, len(q)-1); j <= n; j++ {
		best = min(best, prev[j])
	}
	if best == math.MaxInt { // c är mycket kortare än frågan
		best = prev[n]
	}
	return best
}
