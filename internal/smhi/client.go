// Package smhi är en klient för SMHI:s öppna meteorologiska observationer
// (metobs). Den hämtar bara vindhastigheten, som används för att räkna ut
// hur kall temperaturen känns (se package wind).
//
// Villkor som klienten följer (SMHI:s öppna data, CC BY 4.0):
//   - Bara dokumenterade API:er används.
//   - "Källa: SMHI" och att vinden har bearbetats anges i sidfoten.
//   - Alla stationer hämtas i ett enda anrop, högst en gång per RefreshInterval.
package smhi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"temperatur/internal/httpbody"
)

// latestWindURL ger senaste timmens vindhastighet för alla stationer i ett
// anrop. Parameter 4 är medelvärdet över 10 minuter på 10 m höjd, vilket är
// den vind som formeln för vindkyla förutsätter.
const latestWindURL = "https://opendata-download-metobs.smhi.se/api/version/1.0/parameter/4/station-set/all/period/latest-hour/data.json"

// RefreshInterval är hur ofta vinden hämtas. SMHI mäter en gång i timmen och
// låter svaret cachas i tio minuter, så tätare än så ger inget.
const RefreshInterval = 30 * time.Minute

const (
	requestTimeout    = 30 * time.Second
	maxResponseBytes  = 1 << 20 // ett svar med alla stationer är runt 50 kB
	maxErrorBodyBytes = 64 << 10

	// approvedQuality betyder "kontrollerat och godkänt". Misstänkta och
	// aggregerade värden ("Y") används inte.
	approvedQuality = "G"

	// maxPlausibleSpeed skyddar mot uppenbart felaktiga värden i m/s.
	maxPlausibleSpeed = 75
)

// Wind är den senaste vinden vid en station.
type Wind struct {
	Speed      float64 // m/s
	MeasuredAt time.Time
}

// Client anropar SMHI:s API.
type Client struct {
	http *http.Client
	url  string
}

// NewClient skapar en klient. Är httpClient nil används en med tidsgräns.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	return &Client{http: httpClient, url: latestWindURL}
}

// LatestWind hämtar den senaste godkända vinden per station, med stationens
// nyckel hos SMHI som nyckel. Ett svar utan något godkänt värde är ett fel,
// så att ett tomt svar aldrig ersätter vind som redan har hämtats.
func (c *Client) LatestWind(ctx context.Context) (map[string]Wind, error) {
	var resp latestResponse
	if err := c.get(ctx, &resp); err != nil {
		return nil, err
	}
	winds := resp.approvedWinds()
	if len(winds) == 0 {
		return nil, errors.New("smhi: svaret innehöll ingen godkänd vind")
	}
	return winds, nil
}

func (c *Client) get(ctx context.Context, out *latestResponse) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "temperatur-sok/1.0")
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("smhi: anrop misslyckades: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		httpbody.Drain(res.Body, maxErrorBodyBytes)
		return fmt.Errorf("smhi: oväntad status %s", res.Status)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxResponseBytes)).Decode(out); err != nil {
		return fmt.Errorf("smhi: kunde inte tolka svaret: %w", err)
	}
	return nil
}

type latestResponse struct {
	Station []stationValues `json:"station"`
}

type stationValues struct {
	Key   string        `json:"key"`
	Value []observation `json:"value"`
}

type observation struct {
	Date    int64  `json:"date"` // millisekunder sedan 1970, UTC
	Value   string `json:"value"`
	Quality string `json:"quality"`
}

func (r latestResponse) approvedWinds() map[string]Wind {
	winds := make(map[string]Wind, len(r.Station))
	for _, s := range r.Station {
		if len(s.Value) == 0 || s.Key == "" {
			continue
		}
		if w, ok := s.Value[len(s.Value)-1].wind(); ok {
			winds[s.Key] = w
		}
	}
	return winds
}

func (o observation) wind() (Wind, bool) {
	if o.Quality != approvedQuality || o.Date <= 0 {
		return Wind{}, false
	}
	speed, err := strconv.ParseFloat(o.Value, 64)
	if err != nil || math.IsNaN(speed) || speed < 0 || speed > maxPlausibleSpeed {
		return Wind{}, false
	}
	return Wind{Speed: speed, MeasuredAt: time.UnixMilli(o.Date).UTC()}, true
}
