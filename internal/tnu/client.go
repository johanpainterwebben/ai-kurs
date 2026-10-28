// Package tnu är en klient för temperatur.nu:s API (version 1.20).
//
// Villkor som klienten följer (https://www.temperatur.nu/info/api/):
//   - cli skickas alltid med.
//   - Anropen signeras när en klientnyckel finns; sign läggs sist i URL:en.
//   - Utan nyckel begränsar klienten sig själv till under ~30 anrop per timme.
//
// Att samma fråga inte ställs oftare än var femte minut sköts av feed.Feed,
// som är den enda som anropar klienten.
package tnu

import (
	"context"
	"crypto/md5" //nolint:gosec // MD5 är föreskrivet av API:ets signering
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // tidszonen Europe/Stockholm; distroless-avbildningen saknar zoneinfo

	"temperatur/internal/budget"
	"temperatur/internal/obs"
)

// SourceName är det namn mätpunkterna får i obs.Station.Source.
const SourceName = "temperatur.nu"

// endpoint är API:ets adress utan schema, eftersom det är så den ska ingå i signaturen.
const endpoint = "api.temperatur.nu/tnu_1.20.php"

// UnsignedHourlyLimit är hur många anrop klienten tillåter sig per rullande timme
// utan signering. API:et blockerar efter "ca 30", så vi håller marginal.
const UnsignedHourlyLimit = 25

// ErrBlocked betyder att API:et rapporterar att klienten är blockerad.
var ErrBlocked = errors.New("tnu: klienten är blockerad av temperatur.nu")

// Client anropar det riktiga API:et.
type Client struct {
	cli    string
	key    string
	http   *http.Client
	budget *budget.Budget // bara för osignerade anrop; nil när anropen signeras
}

var _ obs.Source = (*Client)(nil)

// NewClient skapar en klient. key får vara tom; då skickas osignerade anrop
// och klienten håller sig under UnsignedHourlyLimit.
func NewClient(cli, key string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	c := &Client{cli: cli, key: key, http: httpClient}
	if key == "" {
		c.budget = budget.New(UnsignedHourlyLimit, time.Hour, nil)
	}
	return c
}

// Signed anger om klienten signerar sina anrop.
func (c *Client) Signed() bool { return c.key != "" }

// AllStations hämtar samtliga mätpunkter (luft) med namn, kommun, län och position.
// Utelämnas p returnerar API:et alla mätpunkter; verbose ger de extra fälten.
func (c *Client) AllStations(ctx context.Context) ([]obs.Station, error) {
	var resp apiResponse
	if err := c.get(ctx, "verbose", &resp); err != nil {
		return nil, err
	}
	return resp.stations(), nil
}

func (c *Client) get(ctx context.Context, params string, out *apiResponse) error {
	if c.budget != nil && !c.budget.Take() {
		return obs.ErrBudgetExhausted
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.buildURL(params), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "temperatur-sok/1.0")
	res, err := c.http.Do(req)
	if err != nil {
		// Felet innehåller annars hela URL:en, med cli (e-postadress) och en
		// signatur som går att återanvända. Sådant ska inte hamna i loggar.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("tnu: anrop misslyckades: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
		return fmt.Errorf("tnu: oväntad status %s", res.Status)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(out); err != nil {
		return fmt.Errorf("tnu: kunde inte tolka svaret: %w", err)
	}
	if out.Client == "blocked" {
		return ErrBlocked
	}
	return nil
}

// buildURL lägger till cli och, om nyckel finns, sign som sista parameter.
func (c *Client) buildURL(params string) string {
	u := endpoint + "?" + params + "&cli=" + url.QueryEscape(c.cli)
	if c.key != "" {
		u += "&sign=" + sign(u, c.key)
	}
	return "https://" + u
}

// sign beräknar md5("<url utan https://>+<nyckel>") enligt API-dokumentationen.
// MD5 är föreskrivet av API:et och skyddar ingenting av våra egna data, så det
// ska inte bytas ut.
func sign(unsignedURL, key string) string {
	sum := md5.Sum([]byte(unsignedURL + "+" + key)) //nolint:gosec // se importen
	return hex.EncodeToString(sum[:])
}

type apiResponse struct {
	Client   string `json:"client"`
	Stations []struct {
		ID         string    `json:"id"`
		Title      string    `json:"title"`
		Temp       flexFloat `json:"temp"`
		Lat        flexFloat `json:"lat"`
		Lon        flexFloat `json:"lon"`
		Kommun     string    `json:"kommun"`
		Lan        string    `json:"lan"`
		LastUpdate string    `json:"lastUpdate"`
		URL        string    `json:"url"`
	} `json:"stations"`
}

// DecodeStations tolkar ett sparat svar från API:et (JSON) till mätpunkter.
// Används av tnumock.
func DecodeStations(r io.Reader) ([]obs.Station, error) {
	var resp apiResponse
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, err
	}
	return resp.stations(), nil
}

func (r apiResponse) stations() []obs.Station {
	out := make([]obs.Station, 0, len(r.Stations))
	for _, s := range r.Stations {
		out = append(out, obs.Station{
			Source:     SourceName,
			ID:         s.ID,
			Title:      strings.TrimSpace(s.Title),
			Temp:       s.Temp.ptr(),
			Lat:        s.Lat.ptr(),
			Lon:        s.Lon.ptr(),
			Kommun:     s.Kommun,
			Lan:        s.Lan,
			MeasuredAt: parseLocalTime(s.LastUpdate),
			URL:        s.URL,
		})
	}
	return out
}

var stockholm = mustLoadLocation("Europe/Stockholm")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic("tnu: " + err.Error())
	}
	return loc
}

// parseLocalTime tolkar API:ets tidsangivelse, svensk lokal tid på formen
// "2006-01-02 15:04:05", och returnerar den i UTC. Ogiltig eller tom tid ger
// nolltid.
func parseLocalTime(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(s), stockholm)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// flexFloat tolkar tal som API:et skickar antingen som tal eller som sträng
// ("15.7", "15,7"). Tomma eller ogiltiga värden ("N/A") blir "saknas".
type flexFloat struct {
	v  float64
	ok bool
}

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	v, err := strconv.ParseFloat(strings.Replace(s, ",", ".", 1), 64)
	*f = flexFloat{v: v, ok: err == nil}
	return nil
}

func (f flexFloat) ptr() *float64 {
	if !f.ok {
		return nil
	}
	v := f.v
	return &v
}
