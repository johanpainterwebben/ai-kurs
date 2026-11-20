package wind

import (
	"math"

	"temperatur/internal/obs"
	"temperatur/internal/smhi"
)

// LatestWinds ger den senast hämtade vinden per vindstation utan att vänta,
// och false om ingen vind har hämtats än (jfr feed.Feed.Latest).
type LatestWinds func() (map[string]smhi.Wind, bool)

// Estimator räknar ut "Känns som" för mätpunkter.
type Estimator struct {
	mapping Mapping
	winds   LatestWinds
}

// NewEstimator kopplar ihop mappningen med den senast hämtade vinden.
func NewEstimator(m Mapping, winds LatestWinds) *Estimator {
	return &Estimator{mapping: m, winds: winds}
}

// FeelsLike returnerar hur kall mätpunktens temperatur känns, avrundad till
// en decimal, eller nil om mätpunkten saknar temperatur. Finns ingen vind
// för mätpunkten är det samma som temperaturen.
func (e *Estimator) FeelsLike(st obs.Station) *float64 {
	if st.Temp == nil {
		return nil
	}
	t := *st.Temp
	if speed, ok := e.windAt(st); ok {
		t = FeelsLike(t, speed)
	}
	t = math.Round(t*10) / 10
	return &t
}

func (e *Estimator) windAt(st obs.Station) (float64, bool) {
	if st.Source != e.mapping.Source {
		return 0, false
	}
	link, ok := e.mapping.Stations[st.ID]
	if !ok {
		return 0, false
	}
	winds, ok := e.winds()
	if !ok {
		return 0, false
	}
	w, ok := winds[link.SMHIStation]
	return w.Speed, ok
}
