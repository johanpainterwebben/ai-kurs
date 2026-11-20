package main

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"

	"temperatur/internal/obs"
	"temperatur/internal/wind"
)

func at(id string, lat, lon float64) obs.Station {
	return obs.Station{ID: id, Lat: &lat, Lon: &lon}
}

// Uppsala och Stockholm ligger cirka 64 km från varandra.
var testStations = []smhiStation{
	{Key: "97510", Name: "Uppsala Aut", Latitude: 59.8471, Longitude: 17.6320, Active: true},
	{Key: "98210", Name: "Stockholm", Latitude: 59.3417, Longitude: 18.0549, Active: true},
	{Key: "1", Name: "Nedlagd mitt i Uppsala", Latitude: 59.8586, Longitude: 17.6389, Active: false},
}

func TestBuildLinksPicksNearestActiveStationWithinLimit(t *testing.T) {
	points := []obs.Station{
		at("uppsala-centrum", 59.8586, 17.6389), // närmast en nedlagd station
		at("solna", 59.36, 18.0),
		at("kiruna", 67.85, 20.22), // ingen station inom gränsen
		{ID: "utan-position"},
	}
	links := buildLinks(points, testStations, 30)

	if got := links["uppsala-centrum"].SMHIStation; got != "97510" {
		t.Errorf("uppsala-centrum → %q, vill ha den närmaste aktiva (97510)", got)
	}
	if got := links["solna"].SMHIStation; got != "98210" {
		t.Errorf("solna → %q, vill ha 98210", got)
	}
	if _, ok := links["kiruna"]; ok {
		t.Error("kiruna har ingen station inom 30 km och ska inte vara med")
	}
	if _, ok := links["utan-position"]; ok {
		t.Error("en mätpunkt utan position ska inte vara med")
	}
	if km := links["uppsala-centrum"].DistanceKm; km <= 0 || km > 2 {
		t.Errorf("avståndet %v km är orimligt för Uppsala centrum", km)
	}
}

func TestDistanceKm(t *testing.T) {
	if km := distanceKm(59.8471, 17.6320, 59.3417, 18.0549); math.Abs(km-60) > 5 {
		t.Errorf("Uppsala–Stockholm = %.1f km, vill ha cirka 60", km)
	}
	if km := distanceKm(59, 17, 59, 17); km != 0 {
		t.Errorf("samma punkt = %v km, vill ha 0", km)
	}
}

func TestEncodeIsStableAndReadable(t *testing.T) {
	m := wind.Mapping{
		CreatedAt: "2026-11-20", Source: "temperatur.nu", SMHIParameter: 4, MaxDistanceKm: 30,
		Stations: map[string]wind.Link{
			"b": {SMHIStation: "2", DistanceKm: 2.5},
			"a": {SMHIStation: "1", DistanceKm: 1.5},
		},
	}
	var first, second bytes.Buffer
	if err := encode(&first, m); err != nil {
		t.Fatal(err)
	}
	if err := encode(&second, m); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() {
		t.Fatal("två körningar med samma indata ska ge samma fil")
	}
	want := `{"createdAt":"2026-11-20","source":"temperatur.nu","smhiParameter":4,"maxDistanceKm":30,"stations":{
"a":{"smhiStation":"1","distanceKm":1.5},
"b":{"smhiStation":"2","distanceKm":2.5}
}}
`
	if first.String() != want {
		t.Errorf("fick\n%s\nvill ha\n%s", first.String(), want)
	}
	var back wind.Mapping
	if err := json.Unmarshal(first.Bytes(), &back); err != nil || back.Stations["b"].SMHIStation != "2" {
		t.Errorf("filen går inte att läsa tillbaka: %v %+v", err, back)
	}
}
