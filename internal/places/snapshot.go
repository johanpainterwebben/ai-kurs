package places

import (
	"cmp"
	"slices"
	"time"

	"temperatur/internal/obs"
)

// Kind är vilken sorts plats man kan söka på.
type Kind string

const (
	KindKommun Kind = "kommun"
	KindOrt    Kind = "ort"
	KindPlats  Kind = "plats"
)

// order ger kommuner företräde framför orter, och orter framför platser.
func (k Kind) order() int {
	switch k {
	case KindKommun:
		return 0
	case KindOrt:
		return 1
	default:
		return 2
	}
}

// Snapshot är en hämtad lista med mätpunkter, förberedd för sökning.
type Snapshot struct {
	FetchedAt time.Time

	stations []obs.Station
	places   []place
}

// Place är en plats man kan söka på: en kommun, en ort eller en plats inom en
// ort (delen efter snedstrecket i "Lund/Tre Högars väg").
type Place struct {
	Label  string `json:"label"`  // det som visas, t.ex. "Tre Högars väg"
	Query  string `json:"query"`  // det som söks på för exakt denna plats, t.ex. "Lund/Tre Högars väg"
	Detail string `json:"detail"` // län för kommuner, kommun för orter, ort och kommun för platser
	Kind   Kind   `json:"kind"`
	Count  int    `json:"count"` // antal mätpunkter
}

type place struct {
	Place
	names    []string // vikta namn som räknas som exakt träff
	full     matchKey // hela namnet, t.ex. "lund tre hogars vag"
	own      matchKey // för platser bara platsens namn, t.ex. "tre hogars vag"
	stations []int    // index i Snapshot.stations
}

// newSnapshot bygger sökbara platser av mätpunkterna: först grupperas de till
// kommuner, orter och platser, sedan slås dubbletter ihop, och till sist
// görs platserna redo för sökning.
func newSnapshot(stations []obs.Station, fetchedAt time.Time) *Snapshot {
	g := groupStations(stations)
	g.mergeSameNamedPlatser()
	s := &Snapshot{FetchedAt: fetchedAt, stations: stations}
	s.places = append(s.places, g.kommunPlaces()...)
	s.places = append(s.places, g.ortPlaces()...)
	s.places = append(s.places, g.platsPlaces()...)
	s.finalize()
	return s
}

// groups är mätpunkterna grupperade på tre nivåer, med vikta namn som nyckel.
type groups struct {
	stations []obs.Station
	kommuner map[string]*place
	orter    map[string]*place
	platser  map[string]*place
}

func groupStations(stations []obs.Station) *groups {
	g := &groups{
		stations: stations,
		kommuner: map[string]*place{},
		orter:    map[string]*place{},
		platser:  map[string]*place{},
	}
	add := func(m map[string]*place, key string, i int, create func() *place) {
		p := m[key]
		if p == nil {
			p = create()
			m[key] = p
		}
		p.stations = append(p.stations, i)
	}

	for i, st := range stations {
		if st.Kommun != "" {
			add(g.kommuner, st.Kommun, i, func() *place {
				return &place{Place: Place{Label: st.Kommun, Detail: st.Lan, Kind: KindKommun}, names: kommunNames(st.Kommun)}
			})
		}
		ort, plats := splitTitle(st.Title)
		if ort != "" {
			add(g.orter, fold(ort), i, func() *place {
				return &place{Place: Place{Label: ort, Kind: KindOrt}, names: []string{fold(ort)}}
			})
		}
		if ort != "" && plats != "" {
			q := ort + "/" + plats
			add(g.platser, fold(q), i, func() *place {
				return &place{Place: Place{Label: plats, Query: q, Kind: KindPlats}, names: []string{fold(q)}}
			})
		}
	}
	return g
}

// sameAsKommun anger att orten heter som kommunen den ligger i ("Lund" i
// Lunds kommun). För besökaren är det samma plats.
func (g *groups) sameAsKommun(ort, kommun string) bool {
	k := g.kommuner[kommun]
	return k != nil && slices.Contains(k.names, fold(ort))
}

// mergeSameNamedPlatser lägger en plats med samma namn som en ort i samma
// kommun till orten ("Norrköping/Svärtinge" och orten Svärtinge är samma
// ställe).
func (g *groups) mergeSameNamedPlatser() {
	for k, p := range g.platser {
		kommun := g.stations[p.stations[0]].Kommun
		if o := g.orter[fold(p.Label)]; o != nil && onlyInKommun(g.stations, o.stations, kommun) {
			o.stations = append(o.stations, p.stations...)
			delete(g.platser, k)
		}
	}
}

func (g *groups) kommunPlaces() []place {
	out := make([]place, 0, len(g.kommuner))
	for _, p := range g.kommuner {
		out = append(out, *p)
	}
	return out
}

// ortPlaces ger orterna. En ort i en enda kommun visar kommunen som detalj,
// och utelämnas helt om den heter som kommunen.
func (g *groups) ortPlaces() []place {
	var out []place
	for _, p := range g.orter {
		ks := map[string]bool{}
		for _, i := range p.stations {
			ks[g.stations[i].Kommun] = true
		}
		if len(ks) == 1 {
			p.Detail = g.stations[p.stations[0]].Kommun
			if g.sameAsKommun(p.Label, p.Detail) {
				continue // visa bara kommunen
			}
		} else {
			p.Detail = "flera kommuner"
		}
		out = append(out, *p)
	}
	return out
}

// platsPlaces ger platserna inom orter. Ett delnamn som bara finns på ett
// ställe ("Tre Högars väg") räcker som exakt sökning; vanliga som "Centrum"
// kräver orten ("Alfta/Centrum").
func (g *groups) platsPlaces() []place {
	subCount := map[string]int{}
	for _, p := range g.platser {
		subCount[fold(p.Label)]++
	}
	var out []place
	for _, p := range g.platser {
		if subCount[fold(p.Label)] == 1 {
			p.names = append(p.names, fold(p.Label))
		}
		st := g.stations[p.stations[0]]
		ort, _ := splitTitle(st.Title)
		p.Detail = st.Kommun
		if !g.sameAsKommun(ort, st.Kommun) {
			p.Detail = ort + " · " + st.Kommun
		}
		out = append(out, *p)
	}
	return out
}

// finalize fyller i det som alla platser har gemensamt (sökväg, antal,
// matchningsnycklar), sorterar mätpunkterna inom varje plats och platserna
// inbördes: kommuner före orter före platser.
func (s *Snapshot) finalize() {
	for i := range s.places {
		p := &s.places[i]
		if p.Query == "" {
			p.Query = p.Label
		}
		slices.SortStableFunc(p.stations, func(a, b int) int { return byTempDesc(s.stations[a], s.stations[b]) })
		p.Count = len(p.stations)
		p.full = newMatchKey(p.Query)
		p.own = newMatchKey(p.Label)
	}
	slices.SortFunc(s.places, func(a, b place) int {
		return cmp.Or(cmp.Compare(a.Kind.order(), b.Kind.order()), cmp.Compare(a.Query, b.Query))
	})
}

// onlyInKommun anger att alla mätpunkterna ligger i kommunen.
func onlyInKommun(stations []obs.Station, idx []int, kommun string) bool {
	for _, i := range idx {
		if stations[i].Kommun != kommun {
			return false
		}
	}
	return true
}

// byTempDesc sorterar varmast först och mätpunkter utan värde sist.
// Lika temperaturer behåller API:ets ordning, som är alfabetisk.
func byTempDesc(a, b obs.Station) int {
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
