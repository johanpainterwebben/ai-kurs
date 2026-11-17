// Package obs är den källneutrala modellen för mätpunkter. Varje källa
// (temperatur.nu, …) översätter sina svar till de här typerna, så att resten
// av programmet inte behöver veta vilken källa en mätpunkt kommer från.
package obs

import (
	"context"
	"errors"
	"time"
)

// ErrBudgetExhausted betyder att en källa själv stoppade anropet för att inte
// överskrida sin anropsgräns. Inget anrop gick iväg.
var ErrBudgetExhausted = errors.New("anropsgränsen för källan är nådd")

// Station är en mätpunkt.
type Station struct {
	Source     string    `json:"source"` // vilken källa mätpunkten kommer från, t.ex. "temperatur.nu"
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Temp       *float64  `json:"temp"` // nil om mätpunkten saknar giltigt värde
	Lat        *float64  `json:"lat"`
	Lon        *float64  `json:"lon"`
	Kommun     string    `json:"kommun"`
	Lan        string    `json:"lan"`
	MeasuredAt time.Time `json:"measuredAt,omitzero"` // i UTC; noll om källan saknar giltig tid
	URL        string    `json:"url"`                 // sida hos källan, för källhänvisning
}

// Source levererar alla mätpunkter som källan har.
type Source interface {
	AllStations(ctx context.Context) ([]Station, error)
}
