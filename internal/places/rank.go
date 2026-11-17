package places

import (
	"cmp"
	"math"
	"slices"

	"temperatur/internal/obs"
)

// SearchResult är mätpunkterna på den plats en sökning ledde till.
type SearchResult struct {
	Place    *Place        `json:"place"` // nil om ingen plats hittades
	Total    int           `json:"total"`
	Average  *float64      `json:"average"`
	Stations []obs.Station `json:"stations"`
}

// Search tar fram den plats frågan avser och returnerar dess mätpunkter,
// varmast först och de utan värde sist, högst limit stycken. Total och
// Average gäller alla platsens mätpunkter.
//
// Platsen är i första hand en med exakt det namnet ("Lund" ger Lunds kommun),
// annars det bästa förslaget om det inte bygger på en felstavning – då
// får besökaren i stället välja bland förslagen.
func (s *Snapshot) Search(query string, limit int) SearchResult {
	res := SearchResult{Stations: []obs.Station{}}
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
	if len(hits) == 0 || hits[0].score.fuzzy() {
		return nil
	}
	// Flera lika bra träffar med samma namn ("centrum") – gissa inte, låt
	// besökaren välja bland förslagen.
	if len(hits) > 1 && hits[1].score.cmp(hits[0].score) == 0 && fold(hits[1].p.Label) == fold(hits[0].p.Label) {
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
	score matchScore
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
			hits = append(hits, hit{p, matchScore{class: classExact}})
		} else if score, ok := m.match(p); ok {
			hits = append(hits, hit{p, score})
		}
	}
	slices.SortFunc(hits, func(a, b hit) int {
		return cmp.Or(
			a.score.cmp(b.score),
			cmp.Compare(a.p.Kind.order(), b.p.Kind.order()),
			cmp.Compare(b.p.Count, a.p.Count),
			cmp.Compare(a.p.Query, b.p.Query),
		)
	})
	return hits
}
