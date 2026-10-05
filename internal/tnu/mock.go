package tnu

import (
	"compress/gzip"
	"context"
	_ "embed"
	"encoding/json"
	"strings"
)

// Ett sparat, bantat svar med alla mätpunkter.
//
//go:embed mockdata/stations.json.gz
var mockStations string

// Mock returnerar sparade mätpunkter i stället för att anropa temperatur.nu.
// Används under utveckling (TNU_MOCK=1) så att anropskvoten inte förbrukas.
type Mock struct {
	stations []Station
}

// NewMock läser in de sparade mätpunkterna.
func NewMock() (*Mock, error) {
	zr, err := gzip.NewReader(strings.NewReader(mockStations))
	if err != nil {
		return nil, err
	}
	var resp apiResponse
	if err := json.NewDecoder(zr).Decode(&resp); err != nil {
		return nil, err
	}
	return &Mock{stations: resp.stations()}, nil
}

// AllStations returnerar de sparade mätpunkterna.
func (m *Mock) AllStations(context.Context) ([]Station, error) {
	return m.stations, nil
}
