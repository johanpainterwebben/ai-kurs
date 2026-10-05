package places

import (
	"cmp"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"

	"temperatur/internal/tnu"
)

// Snapshot är en hämtad lista med mätpunkter, förberedd för sökning.
type Snapshot struct {
	FetchedAt time.Time

	stations []tnu.Station
	places   []place
}

// Place är en plats man kan söka på: en kommun eller en ort.
type Place struct {
	Label  string `json:"label"`  // det som visas och söks på, t.ex. "Bälinge"
	Detail string `json:"detail"` // län för kommuner, kommun för orter
	Kind   string `json:"kind"`   // "kommun" eller "ort"
	Count  int    `json:"count"`  // antal mätpunkter
}

type place struct {
	Place
	names      []string // vikta namn som räknas som exakt träff
	key        []rune   // vikt namn för förslag, inklusive alias
	wordStarts []int
	stations   []int // index i Snapshot.stations
}

// SearchResult är mätpunkterna på den plats en sökning ledde till.
type SearchResult struct {
	Place    *Place        `json:"place"` // nil om ingen plats hittades
	Total    int           `json:"total"`
	Average  *float64      `json:"average"`
	Stations []tnu.Station `json:"stations"`
}

// aliases lägger till sökord för vanliga förkortningar i mätpunkternas namn.
var aliases = map[string]string{
	"sthlm": "stockholm",
	"gbg":   "goteborg",
}

var foldReplacer = strings.NewReplacer(
	"å", "a", "ä", "a", "ö", "o", "æ", "a", "ø", "o",
	"é", "e", "è", "e", "ü", "u",
	"-", " ", "/", " ", "_", " ", ",", " ", ".", " ", "(", " ", ")", " ",
)

// fold gör text jämförbar oavsett skiftläge, å/ä/ö och skiljetecken:
// "Göteborg/Tuve-Norum" blir "goteborg tuve norum".
func fold(s string) string {
	return strings.Join(strings.Fields(foldReplacer.Replace(strings.ToLower(s))), " ")
}

// withAliases lägger till alias för förkortningar som finns i texten.
func withAliases(folded string) string {
	for _, w := range strings.Fields(folded) {
		if a, ok := aliases[w]; ok {
			folded += " " + a
		}
	}
	return folded
}

// irregularKommuner är kommuner vars ort inte heter som kommunen utan genitiv-s.
var irregularKommuner = map[string]string{
	"falu": "falun",
}

// kommunNames ger de namn en kommun kan sökas på exakt:
// "Lunds kommun" → "lunds kommun", "lunds", "lund".
func kommunNames(label string) []string {
	base := strings.TrimSuffix(fold(label), " kommun")
	names := []string{fold(label), base}
	if s, ok := strings.CutSuffix(base, "s"); ok {
		names = append(names, s)
	}
	if ort, ok := irregularKommuner[base]; ok {
		names = append(names, ort)
	}
	return names
}

// ortSuffix är tillägg som skiljer flera mätpunkter på samma ort åt:
// "Bollnäs 2", "Lysekil2", "Mölnbo (VV)", "Sigtuna vv".
var ortSuffix = regexp.MustCompile(`(?i)\s*(\d+|\(.*\)|\bvv)\s*$`)

// ortName är ortdelen av en mätpunkts namn: "Lund/Värpinge" → "Lund".
func ortName(title string) string {
	ort, _, _ := strings.Cut(title, "/")
	return strings.TrimSpace(ortSuffix.ReplaceAllString(ort, ""))
}

func newSnapshot(stations []tnu.Station, fetchedAt time.Time) *Snapshot {
	kommuner := map[string]*place{}
	orter := map[string]*place{}
	for i, st := range stations {
		if st.Kommun != "" {
			p := kommuner[st.Kommun]
			if p == nil {
				p = &place{Place: Place{Label: st.Kommun, Detail: st.Lan, Kind: "kommun"}, names: kommunNames(st.Kommun)}
				kommuner[st.Kommun] = p
			}
			p.stations = append(p.stations, i)
		}
		if ort := ortName(st.Title); ort != "" {
			k := fold(ort)
			p := orter[k]
			if p == nil {
				p = &place{Place: Place{Label: ort, Kind: "ort"}, names: []string{k}}
				orter[k] = p
			}
			p.stations = append(p.stations, i)
		}
	}

	s := &Snapshot{FetchedAt: fetchedAt, stations: stations}
	for _, p := range kommuner {
		s.places = append(s.places, *p)
	}
	for _, p := range orter {
		ks := map[string]bool{}
		for _, i := range p.stations {
			ks[stations[i].Kommun] = true
		}
		if len(ks) == 1 {
			p.Detail = stations[p.stations[0]].Kommun
			// En ort som har samma namn som kommunen den ligger i är samma
			// plats för besökaren ("Lund" och "Lunds kommun"); visa bara kommunen.
			if k := kommuner[p.Detail]; k != nil && slices.Contains(k.names, p.names[0]) {
				continue
			}
		} else {
			p.Detail = "flera kommuner"
		}
		s.places = append(s.places, *p)
	}

	for i := range s.places {
		p := &s.places[i]
		p.Count = len(p.stations)
		p.key = []rune(withAliases(fold(p.Label)))
		p.wordStarts = []int{0}
		for j, r := range p.key {
			if r == ' ' {
				p.wordStarts = append(p.wordStarts, j+1)
			}
		}
	}
	slices.SortFunc(s.places, func(a, b place) int {
		return cmp.Or(cmp.Compare(kindOrder(a.Kind), kindOrder(b.Kind)), cmp.Compare(a.Label, b.Label))
	})
	return s
}

// Search tar fram den plats frågan avser och returnerar dess mätpunkter,
// högst limit stycken. Total och Average gäller alla platsens mätpunkter.
//
// Platsen är i första hand en med exakt det namnet ("Lund" ger Lunds kommun),
// annars det bästa förslaget om det inte bygger på en felstavning – då
// får besökaren i stället välja bland förslagen.
func (s *Snapshot) Search(query string, limit int) SearchResult {
	res := SearchResult{Stations: []tnu.Station{}}
	p := s.resolve(query)
	if p == nil {
		return res
	}
	res.Place = &p.Place
	res.Total = len(p.stations)
	var sum float64
	var n int
	for _, i := range p.stations {
		st := s.stations[i]
		if len(res.Stations) < limit {
			res.Stations = append(res.Stations, st)
		}
		if st.Temp != nil {
			sum += *st.Temp
			n++
		}
	}
	if n > 0 {
		avg := math.Round(sum/float64(n)*10) / 10
		res.Average = &avg
	}
	return res
}

func (s *Snapshot) resolve(query string) *place {
	q := fold(query)
	if q == "" {
		return nil
	}
	// places är sorterad med kommuner först, så de vinner vid samma namn.
	for i := range s.places {
		if slices.Contains(s.places[i].names, q) {
			return &s.places[i]
		}
	}
	if hits := s.rank(q); len(hits) > 0 && hits[0].score <= scoreContains {
		return hits[0].p
	}
	return nil
}

// Suggest returnerar de platser som bäst matchar det som skrivits hittills.
// Felstavningar tolereras (se maxTypos).
func (s *Snapshot) Suggest(query string, limit int) []Place {
	hits := s.rank(fold(query))
	out := make([]Place, 0, min(limit, len(hits)))
	for _, h := range hits[:min(limit, len(hits))] {
		out = append(out, h.p.Place)
	}
	return out
}

type hit struct {
	p     *place
	score int
}

// rank returnerar alla platser som matchar den vikta frågan q, bäst först.
func (s *Snapshot) rank(q string) []hit {
	if q == "" {
		return nil
	}
	qr := []rune(q)
	typos := maxTypos(len(qr))
	var hits []hit
	for i := range s.places {
		p := &s.places[i]
		score, ok := matchScore(qr, p.key, p.wordStarts, typos)
		if slices.Contains(p.names, q) {
			score, ok = scoreExact, true
		}
		if ok {
			hits = append(hits, hit{p, score})
		}
	}
	slices.SortFunc(hits, func(a, b hit) int {
		return cmp.Or(
			cmp.Compare(a.score, b.score),
			cmp.Compare(kindOrder(a.p.Kind), kindOrder(b.p.Kind)),
			cmp.Compare(b.p.Count, a.p.Count),
			cmp.Compare(a.p.Label, b.p.Label),
		)
	})
	return hits
}

func kindOrder(kind string) int {
	if kind == "kommun" {
		return 0
	}
	return 1
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

// Poäng för hur väl en fråga matchar en plats; lägre är bättre.
const (
	scoreExact    = -1 // platsens namn ("lund" för Lunds kommun)
	scorePrefix   = 0  // början av namnet
	scoreWord     = 1  // början av ett ord i namnet
	scoreContains = 2  // någonstans i namnet
	// scoreContains + n = början av namnet eller ett ord med n skrivfel
)

// matchScore rangordnar hur väl q matchar key, se konstanterna ovan.
func matchScore(q, key []rune, wordStarts []int, typos int) (int, bool) {
	qs, ks := string(q), string(key)
	switch {
	case strings.HasPrefix(ks, qs):
		return scorePrefix, true
	case strings.Contains(ks, " "+qs):
		return scoreWord, true
	case strings.Contains(ks, qs):
		return scoreContains, true
	case typos == 0:
		return 0, false
	}
	best := typos + 1
	for _, start := range wordStarts {
		best = min(best, prefixDistance(q, key[start:]))
	}
	if best > typos {
		return 0, false
	}
	return scoreContains + best, true
}

// prefixDistance är det minsta redigeringsavståndet (med byte av två
// intilliggande tecken som ett fel) mellan q och någon början av c vars
// längd skiljer sig högst ett tecken från q. "upsa" mot "uppsala" ger 1.
func prefixDistance(q, c []rune) int {
	c = c[:min(len(c), len(q)+1)]
	// d[i][j] = avstånd mellan q[:i] och c[:j]
	d := make([][]int, len(q)+1)
	for i := range d {
		d[i] = make([]int, len(c)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(q); i++ {
		for j := 1; j <= len(c); j++ {
			cost := 1
			if q[i-1] == c[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && q[i-1] == c[j-2] && q[i-2] == c[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	best := math.MaxInt
	for j := max(0, len(q)-1); j <= len(c); j++ {
		best = min(best, d[len(q)][j])
	}
	if best == math.MaxInt { // c är mycket kortare än q
		best = d[len(q)][len(c)]
	}
	return best
}
