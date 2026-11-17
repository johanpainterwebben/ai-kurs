package places

import (
	"testing"
	"time"

	"temperatur/internal/obs"
)

// Små handgjorda mätpunkter som visar reglerna för hur platser byggs. De
// ändras inte när de sparade svaren i tnumock uppdateras.

func st(id, title, kommun, lan string, temp *float64) obs.Station {
	return obs.Station{ID: id, Title: title, Kommun: kommun, Lan: lan, Temp: temp}
}

func temp(v float64) *float64 { return &v }

func fixtureSnapshot() *Snapshot {
	return newSnapshot([]obs.Station{
		st("a", "Lund", "Lunds kommun", "Skåne län", temp(10)),
		st("b", "Lund/Värpinge", "Lunds kommun", "Skåne län", temp(12)),
		st("c", "Lund/Centrum", "Lunds kommun", "Skåne län", temp(11)),
		st("g", "Lund/Dörröd", "Lunds kommun", "Skåne län", nil),
		st("d", "Alfta/Centrum", "Ovanåkers kommun", "Gävleborgs län", temp(5)),
		st("e", "Falun", "Falu kommun", "Dalarnas län", temp(3)),
		st("f", "Sthlm/Söder", "Stockholms kommun", "Stockholms län", temp(7)),
		st("h", "Norrköping/Svärtinge", "Norrköpings kommun", "Östergötlands län", temp(8)),
		st("i", "Svärtinge", "Norrköpings kommun", "Östergötlands län", temp(9)),
	}, time.Now())
}

func TestFixtureKommunCollectsItsStations(t *testing.T) {
	res := fixtureSnapshot().Search("lund", 100)
	if res.Place == nil || res.Place.Label != "Lunds kommun" || res.Place.Kind != KindKommun {
		t.Fatalf("Search(lund) = %+v", res.Place)
	}
	if res.Total != 4 || res.Place.Count != 4 || res.Place.Detail != "Skåne län" {
		t.Errorf("total=%d count=%d detail=%q", res.Total, res.Place.Count, res.Place.Detail)
	}
	// Varmast först, utan värde sist, och snittet räknar bara mätpunkter med värde.
	if res.Stations[0].ID != "b" || res.Stations[3].ID != "g" {
		t.Errorf("ordning: %s ... %s", res.Stations[0].ID, res.Stations[3].ID)
	}
	if res.Average == nil || *res.Average != 11 {
		t.Errorf("snitt = %v, vill ha 11", res.Average)
	}
}

func TestFixtureOrtWithKommunNameIsNotListedTwice(t *testing.T) {
	for _, p := range fixtureSnapshot().Suggest("lund", 8) {
		if p.Kind == KindOrt {
			t.Errorf("orten %q ska döljas bakom kommunen med samma namn", p.Label)
		}
	}
}

func TestFixtureUniqueSubLocationIsFoundByItsOwnName(t *testing.T) {
	res := fixtureSnapshot().Search("värpinge", 100)
	if res.Place == nil || res.Place.Kind != KindPlats || res.Place.Query != "Lund/Värpinge" || res.Total != 1 {
		t.Fatalf("Search(värpinge) = %+v", res.Place)
	}
}

func TestFixtureSharedSubLocationNeedsTheOrt(t *testing.T) {
	s := fixtureSnapshot()
	if res := s.Search("centrum", 100); res.Place != nil {
		t.Errorf("centrum finns på två ställen och ska inte gissas: %+v", res.Place)
	}
	got := s.Suggest("centrum", 8)
	if len(got) != 2 {
		t.Fatalf("Suggest(centrum) = %+v", got)
	}
	details := map[string]bool{}
	for _, p := range got {
		details[p.Detail] = true
	}
	// Lund ligger i Lunds kommun och nämns därför inte särskilt.
	if !details["Lunds kommun"] || !details["Alfta · Ovanåkers kommun"] {
		t.Errorf("förslagen ska skilja orterna åt: %+v", got)
	}
	if res := s.Search("Alfta/Centrum", 100); res.Place == nil || res.Total != 1 {
		t.Errorf("Alfta/Centrum ska ge exakt en plats: %+v", res.Place)
	}
}

func TestFixtureIrregularKommun(t *testing.T) {
	got := fixtureSnapshot().Suggest("falun", 8)
	if len(got) != 1 || got[0].Label != "Falu kommun" {
		t.Errorf("Suggest(falun) = %v, vill ha bara Falu kommun", labels(got))
	}
}

func TestFixtureAlias(t *testing.T) {
	got := fixtureSnapshot().Suggest("stockholm", 8)
	if !contains(got, "Sthlm") || !contains(got, "Stockholms kommun") {
		t.Errorf("Suggest(stockholm) = %v, vill ha Sthlm och Stockholms kommun", labels(got))
	}
}

func TestFixtureSameNamedSubLocationJoinsOrt(t *testing.T) {
	s := fixtureSnapshot()
	n := 0
	for _, p := range s.Suggest("svärtinge", 8) {
		if p.Label == "Svärtinge" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("Svärtinge ska finnas en gång, fanns %d", n)
	}
	if res := s.Search("svärtinge", 100); res.Place == nil || res.Total != 2 {
		t.Errorf("orten ska ha båda mätpunkterna: %+v", res)
	}
}

func TestFixturePlacesAreSortedByKind(t *testing.T) {
	s := fixtureSnapshot()
	for i := 1; i < len(s.places); i++ {
		if s.places[i-1].Kind.order() > s.places[i].Kind.order() {
			t.Fatalf("%s (%s) står före %s (%s)", s.places[i-1].Query, s.places[i-1].Kind, s.places[i].Query, s.places[i].Kind)
		}
	}
}
