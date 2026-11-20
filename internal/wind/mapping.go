package wind

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// mappingJSON skapas med cmd/windmap och checkas in. Den hämtas aldrig när
// servern körs.
//
//go:embed mapping.json
var mappingJSON []byte

// Mapping talar om vilken vindstation hos SMHI som hör till varje mätpunkt.
// Den skapas en gång och ändras bara när cmd/windmap körs igen.
type Mapping struct {
	CreatedAt     string          `json:"createdAt"`
	Source        string          `json:"source"`        // källan mätpunkterna kommer från, t.ex. "temperatur.nu"
	SMHIParameter int             `json:"smhiParameter"` // vilken parameter stationerna valdes för
	MaxDistanceKm float64         `json:"maxDistanceKm"`
	Stations      map[string]Link `json:"stations"` // mätpunktens id → vindstation
}

// Link är vindstationen för en mätpunkt.
type Link struct {
	SMHIStation string  `json:"smhiStation"` // stationens nyckel hos SMHI
	DistanceKm  float64 `json:"distanceKm"`
}

// LoadMapping läser den inbäddade mappningen.
func LoadMapping() (Mapping, error) {
	var m Mapping
	if err := json.Unmarshal(mappingJSON, &m); err != nil {
		return Mapping{}, fmt.Errorf("wind: kunde inte läsa mappningen: %w", err)
	}
	return m, nil
}
