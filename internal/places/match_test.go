package places

import (
	"context"
	"testing"
	"time"

	"temperatur/internal/tnu"
)

func realSnapshot(t *testing.T) *Snapshot {
	t.Helper()
	m, err := tnu.NewMock()
	if err != nil {
		t.Fatal(err)
	}
	stations, _ := m.AllStations(context.Background())
	return newSnapshot(stations, time.Now())
}

func labels(ps []Place) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Label
	}
	return out
}

func contains(ps []Place, label string) bool {
	for _, p := range ps {
		if p.Label == label {
			return true
		}
	}
	return false
}

func TestSearchResolvesToOnePlace(t *testing.T) {
	s := realSnapshot(t)
	tests := []struct {
		query, label, kind string
		total              int
	}{
		{"Lund", "Lunds kommun", "kommun", 10}, // orten Lund är sammanslagen med kommunen
		{"lunds kommun", "Lunds kommun", "kommun", 10},
		{"lun", "Lunds kommun", "kommun", 10},       // början av namnet räcker
		{"Uppsala", "Uppsala kommun", "kommun", 26}, // samma 26 som API:ets egen sökning
		{"UPPSALA ", "Uppsala kommun", "kommun", 26},
		{"malmo", "Malmö kommun", "kommun", 15},
		{"Bälinge", "Bälinge", "ort", 2},
		{"Sthlm", "Sthlm", "ort", 69},
		{"stockholm", "Stockholms kommun", "kommun", 26},
		{"Göteborg", "Göteborgs kommun", "kommun", 23},
	}
	for _, tt := range tests {
		res := s.Search(tt.query, 100)
		if res.Place == nil {
			t.Errorf("Search(%q): ingen plats", tt.query)
			continue
		}
		if res.Place.Label != tt.label || res.Place.Kind != tt.kind || res.Total != tt.total {
			t.Errorf("Search(%q) = %s (%s, %d st), vill ha %s (%s, %d st)",
				tt.query, res.Place.Label, res.Place.Kind, res.Total, tt.label, tt.kind, tt.total)
		}
	}
}

func TestSearchOnlyReturnsStationsInPlace(t *testing.T) {
	s := realSnapshot(t)
	for _, st := range s.Search("Lund", 100).Stations {
		if st.Kommun != "Lunds kommun" {
			t.Errorf("Lund gav %s i %s", st.Title, st.Kommun)
		}
	}
	for _, st := range s.Search("Bälinge", 100).Stations {
		if ortName(st.Title) != "Bälinge" {
			t.Errorf("Bälinge gav %s", st.Title)
		}
	}
}

func TestSearchSortsWarmestFirstAndMissingLast(t *testing.T) {
	s := realSnapshot(t)
	for _, q := range []string{"Lund", "Sthlm", "Kiruna", "Göteborg"} {
		res := s.Search(q, 100)
		seenMissing := false
		for i, st := range res.Stations {
			if st.Temp == nil {
				seenMissing = true
				continue
			}
			if seenMissing {
				t.Fatalf("%s: %s har värde men står efter en mätpunkt utan värde", q, st.Title)
			}
			if i > 0 && *st.Temp > *res.Stations[i-1].Temp {
				t.Fatalf("%s: %s (%.1f) står efter %s (%.1f)", q, st.Title, *st.Temp, res.Stations[i-1].Title, *res.Stations[i-1].Temp)
			}
		}
	}
	// Lund har en mätpunkt utan värde (Dörröd); den ska stå sist.
	lund := s.Search("Lund", 100).Stations
	if last := lund[len(lund)-1]; last.Temp != nil {
		t.Errorf("sista mätpunkten i Lund ska sakna värde, fick %s", last.Title)
	}
}

func TestSearchLimitKeepsWarmest(t *testing.T) {
	s := realSnapshot(t)
	all := s.Search("Sthlm", 100).Stations
	top := s.Search("Sthlm", 5).Stations
	for i := range top {
		if top[i].ID != all[i].ID {
			t.Fatalf("med begränsning ska de 5 varmaste visas; plats %d är %s, vill ha %s", i, top[i].Title, all[i].Title)
		}
	}
}

func TestSubLocations(t *testing.T) {
	s := realSnapshot(t)
	tests := []struct {
		query, label, wantQuery string
		total                   int
	}{
		{"tre högars", "Tre Högars väg", "Lund/Tre Högars väg", 1},
		{"tre hogars vag", "Tre Högars väg", "Lund/Tre Högars väg", 1},
		{"Lund/Tre Högars väg", "Tre Högars väg", "Lund/Tre Högars väg", 1},
		{"lund tre", "Tre Högars väg", "Lund/Tre Högars väg", 1},
		{"Alfta/Centrum", "Centrum", "Alfta/Centrum", 1},
		{"hertsön", "Hertsön", "Luleå/Hertsön", 2}, // "Luleå/Hertsön" och "Luleå/Hertsön 2"
	}
	for _, tt := range tests {
		res := s.Search(tt.query, 100)
		if res.Place == nil {
			t.Errorf("Search(%q): ingen plats", tt.query)
			continue
		}
		if res.Place.Kind != "plats" || res.Place.Label != tt.label || res.Place.Query != tt.wantQuery || res.Total != tt.total {
			t.Errorf("Search(%q) = %+v (%d st), vill ha %s / %s (%d st)", tt.query, *res.Place, res.Total, tt.label, tt.wantQuery, tt.total)
		}
	}
	// Det större stället vinner fortfarande vid samma namn.
	if res := s.Search("Lund", 100); res.Place.Label != "Lunds kommun" {
		t.Errorf("Lund ska fortfarande ge kommunen, fick %s", res.Place.Label)
	}
}

func TestAmbiguousSubLocationIsNotGuessed(t *testing.T) {
	s := realSnapshot(t)
	if res := s.Search("centrum", 100); res.Place != nil {
		t.Errorf("centrum finns i många orter och ska inte gissas, fick %s", res.Place.Query)
	}
	got := s.Suggest("centrum", 8)
	if len(got) < 5 || got[0].Label != "Centrum" || got[0].Detail == got[1].Detail {
		t.Errorf("förslagen ska skilja orterna åt: %+v", got)
	}
}

func TestSubLocationsDoNotCrowdOutTowns(t *testing.T) {
	s := realSnapshot(t)
	got := s.Suggest("stockholm", 8)
	for _, want := range []string{"Stockholms kommun", "Stockholm", "Sthlm"} {
		if !contains(got, want) {
			t.Errorf("Suggest(stockholm) = %v, saknar %q", labels(got), want)
		}
	}
	for i, p := range got {
		if p.Kind != "plats" {
			continue
		}
		for _, q := range got[i:] {
			if q.Kind != "plats" {
				t.Errorf("platsen %s står före %s %s", p.Query, q.Kind, q.Label)
			}
		}
	}
}

func TestSameNamedSubLocationMergedIntoOrt(t *testing.T) {
	s := realSnapshot(t)
	n := 0
	for _, p := range s.Suggest("svärtinge", 8) {
		if p.Label == "Svärtinge" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("Svärtinge ska bara finnas en gång bland förslagen, fanns %d", n)
	}
}

func TestSearchTyposAreNotGuessed(t *testing.T) {
	s := realSnapshot(t)
	for _, q := range []string{"upsala", "finnsintealls"} {
		if res := s.Search(q, 100); res.Place != nil || res.Total != 0 || res.Stations == nil {
			t.Errorf("Search(%q) ska inte gissa: %+v", q, res.Place)
		}
	}
}

func TestSearchLimitAndAverage(t *testing.T) {
	s := realSnapshot(t)
	res := s.Search("Sthlm", 5)
	if res.Total != 69 || len(res.Stations) != 5 {
		t.Fatalf("Total=%d, len=%d; vill ha 69 och 5", res.Total, len(res.Stations))
	}
	if res.Average == nil {
		t.Error("snitt saknas")
	}
}

func TestNoDuplicatePlaces(t *testing.T) {
	s := realSnapshot(t)
	got := s.Suggest("lund", 8)
	if contains(got, "Lund") {
		t.Errorf("orten Lund ska inte visas bredvid Lunds kommun: %v", labels(got))
	}
	if got[0].Label != "Lunds kommun" {
		t.Errorf("Lunds kommun ska stå först: %v", labels(got))
	}
	// Göteborg har mätpunkter i flera kommuner och är därför en egen plats.
	if got := s.Suggest("göteborg", 8); !contains(got, "Göteborgs kommun") || !contains(got, "Göteborg") {
		t.Errorf("Suggest(göteborg) = %v", labels(got))
	}
}

func TestSuggest(t *testing.T) {
	s := realSnapshot(t)
	tests := []struct {
		query string
		want  string // ska finnas bland förslagen
		first bool   // och dessutom stå först
	}{
		{"upps", "Uppsala kommun", true},
		{"Uppsala", "Uppsala kommun", true},
		{"upsala", "Uppsala kommun", false},       // ett p för lite
		{"uppsla", "Uppsala kommun", false},       // ett a för lite
		{"linkpoing", "Linköpings kommun", false}, // två bokstäver bytt plats
		{"malmo", "Malmö kommun", true},           // utan prickar
		{"göteborg", "Göteborgs kommun", true},
		{"stockholm", "Sthlm", false}, // alias
		{"bälinge", "Bälinge", true},
	}
	for _, tt := range tests {
		got := s.Suggest(tt.query, 8)
		if !contains(got, tt.want) {
			t.Errorf("Suggest(%q) = %v, saknar %q", tt.query, labels(got), tt.want)
			continue
		}
		if tt.first && got[0].Label != tt.want {
			t.Errorf("Suggest(%q)[0] = %q, vill ha %q", tt.query, got[0].Label, tt.want)
		}
	}
}

func TestSuggestShortQueriesAreExact(t *testing.T) {
	s := realSnapshot(t)
	for _, p := range s.Suggest("xq", 8) {
		t.Errorf("korta frågor ska inte ge luddiga träffar, fick %q", p.Label)
	}
}

func TestSuggestDetails(t *testing.T) {
	s := realSnapshot(t)
	got := s.Suggest("Uppsala kommun", 1)
	if len(got) != 1 || got[0].Kind != "kommun" || got[0].Detail != "Uppsala län" || got[0].Count != 26 {
		t.Errorf("fick %+v", got)
	}
}

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
		if got := prefixDistance([]rune(tt.q), []rune(tt.c)); got != tt.want {
			t.Errorf("prefixDistance(%q, %q) = %d, vill ha %d", tt.q, tt.c, got, tt.want)
		}
	}
}

func TestOrtName(t *testing.T) {
	tests := map[string]string{
		"Lund/Värpinge":       "Lund",
		"Bollnäs 2":           "Bollnäs",
		"Lysekil2":            "Lysekil",
		"Mölnbo (VV)":         "Mölnbo",
		"Sigtuna vv":          "Sigtuna",
		"Luleå/Hertsön 2":     "Luleå",
		"Junosuando Västra 2": "Junosuando Västra",
		"Södra Sandby":        "Södra Sandby",
	}
	for title, want := range tests {
		if got := ortName(title); got != want {
			t.Errorf("ortName(%q) = %q, vill ha %q", title, got, want)
		}
	}
}

func TestIrregularKommunMerged(t *testing.T) {
	s := realSnapshot(t)
	got := s.Suggest("falun", 8)
	if contains(got, "Falun") || len(got) == 0 || got[0].Label != "Falu kommun" {
		t.Errorf("Suggest(falun) = %v, vill ha Falu kommun först och ingen separat Falun", labels(got))
	}
}

func TestFold(t *testing.T) {
	if got := fold("  Göteborg/Tuve-Norum (VV) "); got != "goteborg tuve norum vv" {
		t.Errorf("fold = %q", got)
	}
}
