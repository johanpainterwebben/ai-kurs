package smhi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func serve(t *testing.T, status int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.Client())
	c.url = srv.URL
	return c
}

func savedResponse(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/latest-hour.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLatestWindKeepsOnlyApprovedValues(t *testing.T) {
	winds, err := serve(t, http.StatusOK, savedResponse(t)).LatestWind(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{"97510": 1.5, "140480": 3.0} // Uppsala Aut och Umeå Flygplats
	if len(winds) != len(want) {
		t.Fatalf("fick %d stationer, vill ha %d: %v", len(winds), len(want), winds)
	}
	for key, speed := range want {
		if winds[key].Speed != speed {
			t.Errorf("station %s: %v m/s, vill ha %v", key, winds[key].Speed, speed)
		}
	}
	if _, ok := winds["9013"]; ok {
		t.Error("ett misstänkt värde (kvalitet Y) ska inte användas")
	}
	if _, ok := winds["161940"]; ok {
		t.Error("en station utan värde ska inte vara med")
	}
	measured := winds["97510"].MeasuredAt
	if measured.Location() != time.UTC || !measured.Equal(time.UnixMilli(1795194000000)) {
		t.Errorf("MeasuredAt = %v, vill ha mätningens tid i UTC", measured)
	}
}

func TestLatestWindSkipsImplausibleValues(t *testing.T) {
	body := `{"station":[
		{"key":"1","value":[{"date":1795194000000,"value":"inte ett tal","quality":"G"}]},
		{"key":"2","value":[{"date":1795194000000,"value":"-1","quality":"G"}]},
		{"key":"3","value":[{"date":1795194000000,"value":"120","quality":"G"}]},
		{"key":"4","value":[{"date":0,"value":"3","quality":"G"}]},
		{"key":"5","value":[{"date":1795194000000,"value":"4.2","quality":"G"}]}]}`
	winds, err := serve(t, http.StatusOK, body).LatestWind(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(winds) != 1 || winds["5"].Speed != 4.2 {
		t.Fatalf("fick %v, vill bara ha station 5", winds)
	}
}

func TestLatestWindWithoutApprovedValuesIsAnError(t *testing.T) {
	body := `{"station":[{"key":"1","value":[{"date":1795194000000,"value":"3","quality":"Y"}]}]}`
	if _, err := serve(t, http.StatusOK, body).LatestWind(context.Background()); err == nil {
		t.Fatal("ett svar utan godkänd vind ska ge fel, så att den gamla vinden ligger kvar")
	}
}

func TestLatestWindErrors(t *testing.T) {
	tests := map[string]*Client{
		"felstatus":      serve(t, http.StatusServiceUnavailable, "nere"),
		"trasig JSON":    serve(t, http.StatusOK, `{"station":[`),
		"inte JSON alls": serve(t, http.StatusOK, "<html>"),
	}
	for name, c := range tests {
		if _, err := c.LatestWind(context.Background()); err == nil || !strings.HasPrefix(err.Error(), "smhi:") {
			t.Errorf("%s: err = %v, vill ha ett smhi-fel", name, err)
		}
	}
}
