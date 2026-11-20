package wind

import (
	"math"
	"testing"

	"temperatur/internal/obs"
	"temperatur/internal/smhi"
)

// Värden ur tabellen i SMHI:s faktablad nr 17 (avrundade till hela grader).
func TestFeelsLikeMatchesSMHITable(t *testing.T) {
	tests := []struct{ temp, speed, want float64 }{
		{10, 2, 9},
		{0, 2, -2},
		{-10, 10, -20},
		{-20, 10, -34},
		{-20, 2, -26},
		{-30, 26, -54},
		{4, 14, -3},
	}
	for _, tt := range tests {
		if got := math.Round(FeelsLike(tt.temp, tt.speed)); got != tt.want {
			t.Errorf("FeelsLike(%v °C, %v m/s) = %v, SMHI:s tabell säger %v", tt.temp, tt.speed, got, tt.want)
		}
	}
}

func TestFeelsLikeOutsideFormulaIsTheTemperature(t *testing.T) {
	tests := []struct {
		name        string
		temp, speed float64
	}{
		{"varmare än +10 °C", 10.1, 10},
		{"kallare än −40 °C", -40.1, 10},
		{"nästan vindstilla", -10, 1.2},
		{"starkare än 35 m/s", -10, 35.1},
	}
	for _, tt := range tests {
		if got := FeelsLike(tt.temp, tt.speed); got != tt.temp {
			t.Errorf("%s: FeelsLike = %v, vill ha temperaturen %v", tt.name, got, tt.temp)
		}
	}
	if got := FeelsLike(10, 1.3); got == 10 {
		t.Error("precis på gränserna ska formeln gälla")
	}
}

func ptr(v float64) *float64 { return &v }

func testEstimator(winds map[string]smhi.Wind) *Estimator {
	m := Mapping{Source: "temperatur.nu", Stations: map[string]Link{"uppsala": {SMHIStation: "97510"}}}
	return NewEstimator(m, func() (map[string]smhi.Wind, bool) { return winds, winds != nil })
}

func TestEstimatorUsesWindFromMappedStation(t *testing.T) {
	e := testEstimator(map[string]smhi.Wind{"97510": {Speed: 10}})
	got := e.FeelsLike(obs.Station{Source: "temperatur.nu", ID: "uppsala", Temp: ptr(-10)})
	if got == nil || *got != -20.3 {
		t.Fatalf("FeelsLike = %v, vill ha -20.3", got)
	}
}

func TestEstimatorWithoutWindIsTheTemperature(t *testing.T) {
	tests := map[string]struct {
		e  *Estimator
		st obs.Station
	}{
		"ingen vind hämtad än":         {testEstimator(nil), obs.Station{Source: "temperatur.nu", ID: "uppsala", Temp: ptr(-10)}},
		"mätpunkten saknas i mappning": {testEstimator(map[string]smhi.Wind{"97510": {Speed: 10}}), obs.Station{Source: "temperatur.nu", ID: "ny", Temp: ptr(-10)}},
		"vindstationen saknar värde":   {testEstimator(map[string]smhi.Wind{"1": {Speed: 10}}), obs.Station{Source: "temperatur.nu", ID: "uppsala", Temp: ptr(-10)}},
		"mätpunkt från en annan källa": {testEstimator(map[string]smhi.Wind{"97510": {Speed: 10}}), obs.Station{Source: "annan", ID: "uppsala", Temp: ptr(-10)}},
	}
	for name, tt := range tests {
		if got := tt.e.FeelsLike(tt.st); got == nil || *got != -10 {
			t.Errorf("%s: FeelsLike = %v, vill ha temperaturen -10", name, got)
		}
	}
}

func TestEstimatorWithoutTemperatureIsNil(t *testing.T) {
	e := testEstimator(map[string]smhi.Wind{"97510": {Speed: 10}})
	if got := e.FeelsLike(obs.Station{Source: "temperatur.nu", ID: "uppsala"}); got != nil {
		t.Fatalf("FeelsLike = %v, vill ha nil när temperatur saknas", *got)
	}
}

func TestEmbeddedMappingIsUsable(t *testing.T) {
	m, err := LoadMapping()
	if err != nil {
		t.Fatal(err)
	}
	if m.Source != "temperatur.nu" || m.SMHIParameter != 4 || len(m.Stations) == 0 {
		t.Fatalf("mappningen ser inte rätt ut: källa %q, parameter %d, %d mätpunkter", m.Source, m.SMHIParameter, len(m.Stations))
	}
	for id, link := range m.Stations {
		if link.SMHIStation == "" || link.DistanceKm < 0 || link.DistanceKm > m.MaxDistanceKm {
			t.Errorf("mätpunkten %s: ogiltig koppling %+v", id, link)
		}
	}
}
