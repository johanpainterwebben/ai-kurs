// Package tnumock är en källa med sparade svar från temperatur.nu. Den
// används under utveckling och i tester så att anropskvoten inte förbrukas.
//
// Produktionsbygget (utan taggen "mock") importerar inte paketet, så varken
// koden eller de sparade svaren ingår i den binär som körs på servern.
package tnumock

import (
	"compress/gzip"
	"context"
	_ "embed"
	"strings"

	"temperatur/internal/obs"
	"temperatur/internal/tnu"
)

// Ett sparat, bantat svar med alla mätpunkter.
//
//go:embed stations.json.gz
var savedStations string

// Mock returnerar sparade mätpunkter i stället för att anropa temperatur.nu.
type Mock struct {
	stations []obs.Station
}

var _ obs.Source = (*Mock)(nil)

// New läser in de sparade mätpunkterna.
func New() (*Mock, error) {
	zr, err := gzip.NewReader(strings.NewReader(savedStations))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	stations, err := tnu.DecodeStations(zr)
	if err != nil {
		return nil, err
	}
	return &Mock{stations: stations}, nil
}

// AllStations returnerar de sparade mätpunkterna.
func (m *Mock) AllStations(context.Context) ([]obs.Station, error) {
	return m.stations, nil
}
