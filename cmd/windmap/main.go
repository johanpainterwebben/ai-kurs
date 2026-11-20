// Kommandot windmap skapar internal/wind/mapping.json: vilken vindstation hos
// SMHI som hör till varje mätpunkt hos temperatur.nu. Det körs för hand och
// ingår inte i produktionsbinären.
//
// Mätpunkterna tas från de sparade svaren i internal/tnumock, så inget anrop
// görs mot temperatur.nu. SMHI:s stationslista hämtas med ett anrop, eller
// läses från en sparad fil:
//
//	go run ./cmd/windmap
//	go run ./cmd/windmap -smhi-stations stationer.json
package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"slices"
	"time"

	"temperatur/internal/obs"
	"temperatur/internal/tnu"
	"temperatur/internal/tnumock"
	"temperatur/internal/wind"
)

// windParameter är SMHI:s parameter för vindhastighet (medelvärde 10 min,
// en gång i timmen), samma som servern hämtar.
const windParameter = 4

const (
	stationListURL     = "https://opendata-download-metobs.smhi.se/api/version/1.0/parameter/4.json"
	defaultMaxKm       = 30
	maxStationListSize = 8 << 20
	requestTimeout     = 30 * time.Second
	earthRadiusKm      = 6371.0
)

// smhiStation är en station i SMHI:s stationslista för en parameter.
type smhiStation struct {
	Key       string  `json:"key"`
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Active    bool    `json:"active"`
}

func main() {
	stationsFile := flag.String("smhi-stations", "", "sparad stationslista från SMHI (annars hämtas den)")
	out := flag.String("out", "internal/wind/mapping.json", "fil att skriva")
	maxKm := flag.Float64("max-km", defaultMaxKm, "största avstånd till vindstationen")
	date := flag.String("date", time.Now().Format(time.DateOnly), "datum som skrivs i filen")
	flag.Parse()

	if err := run(*stationsFile, *out, *maxKm, *date); err != nil {
		fmt.Fprintln(os.Stderr, "windmap:", err)
		os.Exit(1)
	}
}

func run(stationsFile, out string, maxKm float64, date string) error {
	points, err := measuringPoints()
	if err != nil {
		return err
	}
	stations, err := loadSMHIStations(stationsFile)
	if err != nil {
		return err
	}
	m := wind.Mapping{
		CreatedAt:     date,
		Source:        tnu.SourceName,
		SMHIParameter: windParameter,
		MaxDistanceKm: maxKm,
		Stations:      buildLinks(points, stations, maxKm),
	}
	var buf bytes.Buffer
	if err := encode(&buf, m); err != nil {
		return err
	}
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil { //nolint:gosec // filen checkas in och är inte hemlig
		return err
	}
	fmt.Printf("%d av %d mätpunkter har en vindstation inom %g km (%s)\n", len(m.Stations), len(points), maxKm, out)
	return nil
}

func measuringPoints() ([]obs.Station, error) {
	src, err := tnumock.New()
	if err != nil {
		return nil, err
	}
	return src.AllStations(context.Background())
}

func loadSMHIStations(file string) ([]smhiStation, error) {
	if file == "" {
		return fetchSMHIStations()
	}
	f, err := os.Open(file) //nolint:gosec // sökvägen anges av den som kör verktyget
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return decodeSMHIStations(f)
}

func fetchSMHIStations() ([]smhiStation, error) {
	client := &http.Client{Timeout: requestTimeout}
	req, err := http.NewRequest(http.MethodGet, stationListURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "temperatur-sok/1.0")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SMHI svarade %s", res.Status)
	}
	return decodeSMHIStations(res.Body)
}

func decodeSMHIStations(r io.Reader) ([]smhiStation, error) {
	var list struct {
		Station []smhiStation `json:"station"`
	}
	if err := json.NewDecoder(io.LimitReader(r, maxStationListSize)).Decode(&list); err != nil {
		return nil, fmt.Errorf("kunde inte tolka SMHI:s stationslista: %w", err)
	}
	if len(list.Station) == 0 {
		return nil, errors.New("SMHI:s stationslista är tom")
	}
	return list.Station, nil
}

// buildLinks kopplar varje mätpunkt till den närmaste aktiva vindstationen
// inom maxKm. Mätpunkter utan position eller utan station inom gränsen
// kommer inte med.
func buildLinks(points []obs.Station, stations []smhiStation, maxKm float64) map[string]wind.Link {
	active := slices.DeleteFunc(slices.Clone(stations), func(s smhiStation) bool { return !s.Active })
	links := make(map[string]wind.Link)
	for _, p := range points {
		if p.Lat == nil || p.Lon == nil {
			continue
		}
		nearest, km, ok := nearestStation(*p.Lat, *p.Lon, active)
		if ok && km <= maxKm {
			links[p.ID] = wind.Link{SMHIStation: nearest.Key, DistanceKm: math.Round(km*10) / 10}
		}
	}
	return links
}

// nearestStation väljer den närmaste stationen. Vid lika avstånd vinner
// den med lägst nyckel, så att resultatet blir detsamma varje gång.
func nearestStation(lat, lon float64, stations []smhiStation) (smhiStation, float64, bool) {
	var best smhiStation
	bestKm := math.Inf(1)
	for _, s := range stations {
		km := distanceKm(lat, lon, s.Latitude, s.Longitude)
		if km < bestKm || (km == bestKm && s.Key < best.Key) {
			best, bestKm = s, km
		}
	}
	return best, bestKm, !math.IsInf(bestKm, 1)
}

// distanceKm är avståndet längs jordytan (haversine).
func distanceKm(lat1, lon1, lat2, lon2 float64) float64 {
	φ1, φ2 := lat1*math.Pi/180, lat2*math.Pi/180
	dφ := φ2 - φ1
	dλ := (lon2 - lon1) * math.Pi / 180
	a := math.Pow(math.Sin(dφ/2), 2) + math.Cos(φ1)*math.Cos(φ2)*math.Pow(math.Sin(dλ/2), 2)
	return 2 * earthRadiusKm * math.Asin(math.Sqrt(a))
}

// encode skriver en mätpunkt per rad, sorterat på id, så att en ny körning
// bara ändrar de rader som faktiskt ändrats.
func encode(w io.Writer, m wind.Mapping) error {
	header, err := json.Marshal(struct {
		CreatedAt     string  `json:"createdAt"`
		Source        string  `json:"source"`
		SMHIParameter int     `json:"smhiParameter"`
		MaxDistanceKm float64 `json:"maxDistanceKm"`
	}{m.CreatedAt, m.Source, m.SMHIParameter, m.MaxDistanceKm})
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(m.Stations))
	for id := range m.Stations {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, cmp.Compare)

	var buf bytes.Buffer
	buf.Write(header[:len(header)-1]) // utan avslutande "}"
	buf.WriteString(`,"stations":{` + "\n")
	for i, id := range ids {
		key, _ := json.Marshal(id)
		link, err := json.Marshal(m.Stations[id])
		if err != nil {
			return err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(link)
		if i < len(ids)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("}}\n")
	_, err = w.Write(buf.Bytes())
	return err
}
