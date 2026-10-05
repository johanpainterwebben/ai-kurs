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

// Place är en plats man kan söka på: en kommun, en ort eller en plats inom en
// ort (delen efter snedstrecket i "Lund/Tre Högars väg").
type Place struct {
	Label  string `json:"label"`  // det som visas, t.ex. "Tre Högars väg"
	Query  string `json:"query"`  // det som söks på för exakt denna plats, t.ex. "Lund/Tre Högars väg"
	Detail string `json:"detail"` // län för kommuner, kommun för orter, ort och kommun för platser
	Kind   string `json:"kind"`   // "kommun", "ort" eller "plats"
	Count  int    `json:"count"`  // antal mätpunkter
}

type place struct {
	Place
	names    []string // vikta namn som räknas som exakt träff
	full     matchKey // hela namnet, t.ex. "lund tre hogars vag"
	own      matchKey // för platser bara platsens namn, t.ex. "tre hogars vag"
	stations []int    // index i Snapshot.stations
}

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

// splitTitle delar en mätpunkts namn i ort och plats inom orten, utan
// tillägg: "Luleå/Hertsön 2" → "Luleå", "Hertsön". Plats är tom om namnet
// saknar snedstreck.
func splitTitle(title string) (ort, plats string) {
	ort, plats, _ = strings.Cut(title, "/")
	clean := func(s string) string { return strings.TrimSpace(ortSuffix.ReplaceAllString(s, "")) }
	return clean(ort), clean(plats)
}

// ortName är ortdelen av en mätpunkts namn: "Lund/Värpinge" → "Lund".
func ortName(title string) string {
	ort, _ := splitTitle(title)
	return ort
}

func newSnapshot(stations []tnu.Station, fetchedAt time.Time) *Snapshot {
	kommuner := map[string]*place{}
	orter := map[string]*place{}
	platser := map[string]*place{}
	group := func(m map[string]*place, key string, i int, create func() *place) {
		p := m[key]
		if p == nil {
			p = create()
			m[key] = p
		}
		p.stations = append(p.stations, i)
	}

	for i, st := range stations {
		if st.Kommun != "" {
			group(kommuner, st.Kommun, i, func() *place {
				return &place{Place: Place{Label: st.Kommun, Detail: st.Lan, Kind: "kommun"}, names: kommunNames(st.Kommun)}
			})
		}
		ort, plats := splitTitle(st.Title)
		if ort != "" {
			group(orter, fold(ort), i, func() *place {
				return &place{Place: Place{Label: ort, Kind: "ort"}, names: []string{fold(ort)}}
			})
		}
		if ort != "" && plats != "" {
			q := ort + "/" + plats
			group(platser, fold(q), i, func() *place {
				return &place{Place: Place{Label: plats, Query: q, Kind: "plats"}, names: []string{fold(q)}}
			})
		}
	}

	// sameAsKommun anger att orten heter som kommunen den ligger i ("Lund" i
	// Lunds kommun). För besökaren är det samma plats.
	sameAsKommun := func(ort, kommun string) bool {
		k := kommuner[kommun]
		return k != nil && slices.Contains(k.names, fold(ort))
	}

	// En plats med samma namn som en ort i samma kommun är samma ställe
	// ("Norrköping/Svärtinge" och orten Svärtinge); lägg den till orten.
	for k, p := range platser {
		kommun := stations[p.stations[0]].Kommun
		if o := orter[fold(p.Label)]; o != nil && onlyInKommun(stations, o.stations, kommun) {
			o.stations = append(o.stations, p.stations...)
			delete(platser, k)
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
			if sameAsKommun(p.Label, p.Detail) {
				continue // visa bara kommunen
			}
		} else {
			p.Detail = "flera kommuner"
		}
		s.places = append(s.places, *p)
	}

	// Ett delnamn som bara finns på ett ställe ("Tre Högars väg") räcker som
	// exakt sökning; vanliga som "Centrum" kräver orten ("Alfta/Centrum").
	subCount := map[string]int{}
	for _, p := range platser {
		subCount[fold(p.Label)]++
	}
	for _, p := range platser {
		if subCount[fold(p.Label)] == 1 {
			p.names = append(p.names, fold(p.Label))
		}
		st := stations[p.stations[0]]
		ort, _ := splitTitle(st.Title)
		p.Detail = st.Kommun
		if !sameAsKommun(ort, st.Kommun) {
			p.Detail = ort + " · " + st.Kommun
		}
		s.places = append(s.places, *p)
	}

	for i := range s.places {
		p := &s.places[i]
		if p.Query == "" {
			p.Query = p.Label
		}
		slices.SortStableFunc(p.stations, func(a, b int) int { return byTempDesc(stations[a], stations[b]) })
		p.Count = len(p.stations)
		p.full = newMatchKey(p.Query)
		p.own = newMatchKey(p.Label)
	}
	slices.SortFunc(s.places, func(a, b place) int {
		return cmp.Or(cmp.Compare(kindOrder(a.Kind), kindOrder(b.Kind)), cmp.Compare(a.Query, b.Query))
	})
	return s
}

// onlyInKommun anger att alla mätpunkterna ligger i kommunen.
func onlyInKommun(stations []tnu.Station, idx []int, kommun string) bool {
	for _, i := range idx {
		if stations[i].Kommun != kommun {
			return false
		}
	}
	return true
}

// byTempDesc sorterar varmast först och mätpunkter utan värde sist.
// Lika temperaturer behåller API:ets ordning, som är alfabetisk.
func byTempDesc(a, b tnu.Station) int {
	switch {
	case a.Temp == nil && b.Temp == nil:
		return 0
	case a.Temp == nil:
		return 1
	case b.Temp == nil:
		return -1
	}
	return cmp.Compare(*b.Temp, *a.Temp)
}

// Search tar fram den plats frågan avser och returnerar dess mätpunkter,
// varmast först och de utan värde sist, högst limit stycken. Total och
// Average gäller alla platsens mätpunkter.
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
	// places är sorterad med kommuner först och platser sist, så den
	// större platsen vinner vid samma namn.
	for i := range s.places {
		if slices.Contains(s.places[i].names, q) {
			return &s.places[i]
		}
	}
	hits := s.rank(q)
	if len(hits) == 0 || hits[0].fuzzy {
		return nil
	}
	// Flera lika bra träffar med samma namn ("centrum") – gissa inte, låt
	// besökaren välja bland förslagen.
	if len(hits) > 1 && hits[1].score == hits[0].score && fold(hits[1].p.Label) == fold(hits[0].p.Label) {
		return nil
	}
	return hits[0].p
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
	fuzzy bool // bygger på en felstavning
}

// rank returnerar alla platser som matchar den vikta frågan q, bäst först.
func (s *Snapshot) rank(q string) []hit {
	if q == "" {
		return nil
	}
	m := newMatcher(q)
	var hits []hit
	for i := range s.places {
		p := &s.places[i]
		if slices.Contains(p.names, q) {
			hits = append(hits, hit{p, scoreExact, false})
		} else if score, fuzzy, ok := m.match(p); ok {
			hits = append(hits, hit{p, score, fuzzy})
		}
	}
	slices.SortFunc(hits, func(a, b hit) int {
		return cmp.Or(
			cmp.Compare(a.score, b.score),
			cmp.Compare(kindOrder(a.p.Kind), kindOrder(b.p.Kind)),
			cmp.Compare(b.p.Count, a.p.Count),
			cmp.Compare(a.p.Query, b.p.Query),
		)
	})
	return hits
}

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
// Högars väg") matchas i första hand på sitt eget namn. Matchar frågan bara
// via orten ("lund", "stockholm") hamnar den efter kommuner och orter, så att
// de inte trängs undan av alla platser i orten.
func (m *matcher) match(p *place) (score int, fuzzy, ok bool) {
	if p.Kind == "plats" {
		if score, ok := m.score(p.own); ok {
			return score, score > scoreContains, true
		}
		if score, ok := m.score(p.full); ok {
			return score + scoreViaOrt, score > scoreContains, true
		}
		return 0, false, false
	}
	score, ok = m.score(p.full)
	return score, score > scoreContains, ok
}

// kindOrder ger kommuner företräde framför orter, och orter framför platser.
func kindOrder(kind string) int {
	switch kind {
	case "kommun":
		return 0
	case "ort":
		return 1
	default:
		return 2
	}
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

	// scoreViaOrt läggs till för en plats som bara matchar via sin ort.
	scoreViaOrt = 3
)

// score rangordnar hur väl frågan matchar k, se konstanterna ovan.
func (m *matcher) score(k matchKey) (int, bool) {
	switch {
	case strings.HasPrefix(k.str, m.qs):
		return scorePrefix, true
	case strings.Contains(k.str, m.qWord):
		return scoreWord, true
	case strings.Contains(k.str, m.qs):
		return scoreContains, true
	case m.typos == 0:
		return 0, false
	}
	best := m.typos + 1
	for _, start := range k.wordStarts {
		best = min(best, m.prefixDistance(k.runes[start:]))
		if best == 1 {
			break // bättre än ett fel går inte, utan exakt träff ovan
		}
	}
	if best > m.typos {
		return 0, false
	}
	return scoreContains + best, true
}

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
