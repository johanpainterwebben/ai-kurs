package places

import (
	"testing"

	"temperatur/internal/obs"
)

// sameAsTemp är "Känns som" utan vind: samma som temperaturen.
func sameAsTemp(st obs.Station) *float64 { return st.Temp }

func TestSearchAddsFeelsLikeAndItsAverage(t *testing.T) {
	colder := func(st obs.Station) *float64 {
		if st.Temp == nil {
			return nil
		}
		v := *st.Temp - 5
		return &v
	}
	all := fixtureSnapshot().Search("lund", 100, sameAsTemp)
	res := fixtureSnapshot().Search("lund", 1, colder)
	if len(res.Stations) != 1 {
		t.Fatalf("förutsättning: vill ha en mätpunkt, fick %d", len(res.Stations))
	}
	st := res.Stations[0]
	if st.Temp == nil || st.FeelsLike == nil || *st.FeelsLike != *st.Temp-5 {
		t.Fatalf("FeelsLike = %v för temperaturen %v, vill ha 5 grader kallare", st.FeelsLike, st.Temp)
	}
	if all.Average == nil || res.AverageFeelsLike == nil || *res.AverageFeelsLike != *all.Average-5 {
		t.Fatalf("AverageFeelsLike = %v, vill ha medel för alla platsens mätpunkter (%v) minus 5", res.AverageFeelsLike, all.Average)
	}
}
