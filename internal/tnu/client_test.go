package tnu

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Exemplet ur API-dokumentationen; förväntat värde framräknat med Pythons hashlib.
func TestSignMatchesDocumentation(t *testing.T) {
	got := sign("api.temperatur.nu/tnu_1.20.php?p=stockholm&verbose&cli=din_klient", "din_hemliga_nyckel")
	if want := "0f041c90e39755212b465c4524e40f4a"; got != want {
		t.Fatalf("sign = %s, vill ha %s", got, want)
	}
}

func TestBuildURL(t *testing.T) {
	unsigned := NewClient("min@epost.se", "", nil)
	if got, want := unsigned.buildURL("search=g%C3%B6teborg&verbose"),
		"https://api.temperatur.nu/tnu_1.20.php?search=g%C3%B6teborg&verbose&cli=min%40epost.se"; got != want {
		t.Errorf("osignerad:\n got %s\nwant %s", got, want)
	}

	signed := NewClient("din_klient", "din_hemliga_nyckel", nil)
	got := signed.buildURL("p=stockholm&verbose")
	want := "https://api.temperatur.nu/tnu_1.20.php?p=stockholm&verbose&cli=din_klient&sign=0f041c90e39755212b465c4524e40f4a"
	if got != want {
		t.Errorf("signerad:\n got %s\nwant %s", got, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fixtureClient(t *testing.T, key string, body string, requests *[]string) *Client {
	t.Helper()
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		*requests = append(*requests, r.URL.String())
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     http.Header{},
		}, nil
	})}
	return NewClient("test", key, hc)
}

func TestAllStationsParsesResponse(t *testing.T) {
	body, err := os.ReadFile("testdata/search_uppsala.json")
	if err != nil {
		t.Fatal(err)
	}
	var reqs []string
	c := fixtureClient(t, "", string(body), &reqs)

	stations, err := c.AllStations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 || reqs[0] != "https://api.temperatur.nu/tnu_1.20.php?verbose&cli=test" {
		t.Fatalf("oväntade anrop: %v", reqs)
	}
	if len(stations) != 26 {
		t.Fatalf("fick %d mätpunkter, vill ha 26", len(stations))
	}
	s := stations[0]
	if s.ID != "askarback" || s.Title != "Askarbäck" || s.Kommun != "Uppsala kommun" ||
		s.Temp == nil || *s.Temp != 15.7 || s.Lat == nil || *s.Lat != 60.031732 ||
		s.URL != "https://www.temperatur.nu/askarback" {
		t.Errorf("första mätpunkten tolkades fel: %+v", s)
	}
	if stations[8].Title != "Lill-moga" {
		t.Errorf("namn ska trimmas: %q", stations[8].Title)
	}
}

func TestAllStationsHandlesMissingValuesAndBlocked(t *testing.T) {
	var reqs []string
	c := fixtureClient(t, "k", `{"client":"signed","stations":[{"id":"x","temp":"N/A","lat":""}]}`, &reqs)
	stations, err := c.AllStations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stations[0].Temp != nil || stations[0].Lat != nil {
		t.Errorf("saknade värden ska bli nil: %+v", stations[0])
	}

	c = fixtureClient(t, "k", `{"client":"blocked","stations":[]}`, &reqs)
	if _, err := c.AllStations(context.Background()); !errors.Is(err, ErrBlocked) {
		t.Errorf("err = %v, vill ha ErrBlocked", err)
	}
}

func TestUnsignedBudget(t *testing.T) {
	var reqs []string
	c := fixtureClient(t, "", `{"client":"unsigned","stations":[]}`, &reqs)
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }

	for i := range UnsignedHourlyLimit {
		if _, err := c.AllStations(context.Background()); err != nil {
			t.Fatalf("anrop %d: %v", i+1, err)
		}
	}
	if _, err := c.AllStations(context.Background()); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("err = %v, vill ha ErrBudgetExhausted", err)
	}
	if len(reqs) != UnsignedHourlyLimit {
		t.Fatalf("%d anrop gick iväg, max är %d", len(reqs), UnsignedHourlyLimit)
	}

	now = now.Add(time.Hour + time.Second)
	if _, err := c.AllStations(context.Background()); err != nil {
		t.Fatalf("efter en timme: %v", err)
	}
}

func TestSignedHasNoLocalBudget(t *testing.T) {
	var reqs []string
	c := fixtureClient(t, "k", `{"client":"signed","stations":[]}`, &reqs)
	for range UnsignedHourlyLimit + 5 {
		if _, err := c.AllStations(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMockLoadsAllStations(t *testing.T) {
	m, err := NewMock()
	if err != nil {
		t.Fatal(err)
	}
	stations, _ := m.AllStations(context.Background())
	if len(stations) < 1000 {
		t.Fatalf("fick bara %d mätpunkter", len(stations))
	}
}
