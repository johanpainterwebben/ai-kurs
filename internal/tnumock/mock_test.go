package tnumock

import (
	"context"
	"testing"
)

func TestMockLoadsAllStations(t *testing.T) {
	m, err := New()
	if err != nil {
		t.Fatal(err)
	}
	stations, _ := m.AllStations(context.Background())
	if len(stations) < 1000 {
		t.Fatalf("fick bara %d mätpunkter", len(stations))
	}
	if stations[0].Source == "" || stations[0].MeasuredAt.IsZero() {
		t.Errorf("källa och mättid ska vara ifyllda: %+v", stations[0])
	}
}
