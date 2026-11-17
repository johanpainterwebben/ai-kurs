package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"temperatur/internal/obs"
	"temperatur/internal/places"
	"temperatur/internal/tnumock"
)

type countingSource struct {
	obs.Source
	calls atomic.Int32
	err   error
}

func (c *countingSource) AllStations(ctx context.Context) ([]obs.Station, error) {
	c.calls.Add(1)
	if c.err != nil {
		return nil, c.err
	}
	return c.Source.AllStations(ctx)
}

func newTestServer(t *testing.T, err error) (http.Handler, *countingSource) {
	t.Helper()
	mock, e := tnumock.New()
	if e != nil {
		t.Fatal(e)
	}
	src := &countingSource{Source: mock, err: err}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	static := fstest.MapFS{
		"index.html":       {Data: []byte("<!doctype html>")},
		"fonts/font.woff2": {Data: []byte("font")},
	}
	srv, e := New(places.NewIndex(src, 5*time.Minute, log), static, log)
	if e != nil {
		t.Fatal(e)
	}
	return srv.Handler(), src
}

func get[T any](t *testing.T, h http.Handler, path, q string) (int, T) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path+"?q="+url.QueryEscape(q), nil))
	var body T
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("%s %q: %v", path, q, err)
	}
	return rec.Code, body
}

func TestManySearchesUseOneAPICall(t *testing.T) {
	h, src := newTestServer(t, nil)
	for _, q := range []string{"Uppsala", "upps", "upsala", "Malmö", "göteborg", "xyzzy"} {
		if code, _ := get[suggestResponse](t, h, "/api/suggest", q); code != http.StatusOK {
			t.Fatalf("suggest %q: %d", q, code)
		}
		if code, _ := get[searchResponse](t, h, "/api/search", q); code != http.StatusOK {
			t.Fatalf("search %q: %d", q, code)
		}
	}
	if n := src.calls.Load(); n != 1 {
		t.Fatalf("API:et anropades %d gånger, vill ha 1", n)
	}
}

func TestSearch(t *testing.T) {
	h, _ := newTestServer(t, nil)
	code, body := get[searchResponse](t, h, "/api/search", "  Uppsala ")
	if code != http.StatusOK || body.Query != "Uppsala" || body.Place == nil || body.Place.Label != "Uppsala kommun" ||
		body.Total != 26 || len(body.Stations) != 26 || body.FetchedAt.IsZero() {
		t.Fatalf("status %d, svar %+v", code, body)
	}
	if len(body.DidYouMean) != 0 {
		t.Errorf("didYouMean ska vara tom när något hittas: %v", body.DidYouMean)
	}
}

func TestSearchDidYouMean(t *testing.T) {
	h, _ := newTestServer(t, nil)
	_, body := get[searchResponse](t, h, "/api/search", "upsala")
	if body.Place != nil || body.Total != 0 {
		t.Fatalf("upsala ska inte gissas, fick %+v", body.Place)
	}
	if len(body.DidYouMean) == 0 || body.DidYouMean[0].Label != "Uppsala kommun" {
		t.Fatalf("didYouMean = %+v", body.DidYouMean)
	}
}

type staleIndex struct{ *places.Index }

func (staleIndex) Stale() bool { return true }

func TestSearchReportsStaleData(t *testing.T) {
	h, _ := newTestServer(t, nil)
	if _, body := get[searchResponse](t, h, "/api/search", "Lund"); body.Stale {
		t.Fatal("stale ska vara false när hämtningarna fungerar")
	}

	mock, _ := tnumock.New()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	idx := staleIndex{places.NewIndex(mock, 5*time.Minute, log)}
	srv, err := New(idx, fstest.MapFS{}, log)
	if err != nil {
		t.Fatal(err)
	}
	h = srv.Handler()
	code, body := get[searchResponse](t, h, "/api/search", "Lund")
	if code != http.StatusOK || !body.Stale || body.Total != 10 {
		t.Fatalf("status %d, stale=%v, total=%d; vill ha 200, true, 10", code, body.Stale, body.Total)
	}
}

func TestValidation(t *testing.T) {
	h, src := newTestServer(t, nil)
	for _, q := range []string{"", " a ", strings.Repeat("a", 51), "\x00\x00", "lu\x07nd", "lund\xff"} {
		if code, _ := get[map[string]string](t, h, "/api/search", q); code != http.StatusBadRequest {
			t.Errorf("search %q: status %d, vill ha 400", q, code)
		}
		if code, body := get[suggestResponse](t, h, "/api/suggest", q); code != http.StatusOK || len(body.Suggestions) != 0 {
			t.Errorf("suggest %q: status %d, %v", q, code, body)
		}
	}
	if n := src.calls.Load(); n != 0 {
		t.Fatalf("ogiltiga frågor ska inte nå API:et, anrop: %d", n)
	}
}

func TestUpstreamErrors(t *testing.T) {
	h, _ := newTestServer(t, obs.ErrBudgetExhausted)
	if code, _ := get[map[string]string](t, h, "/api/search", "lund"); code != http.StatusServiceUnavailable {
		t.Errorf("status %d, vill ha 503", code)
	}
	h, _ = newTestServer(t, io.ErrUnexpectedEOF)
	if code, _ := get[map[string]string](t, h, "/api/suggest", "lund"); code != http.StatusBadGateway {
		t.Errorf("status %d, vill ha 502", code)
	}
}

func TestStaticAndHeaders(t *testing.T) {
	h, _ := newTestServer(t, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("CSP saknas")
	}
}

func TestStaticFiles(t *testing.T) {
	h, _ := newTestServer(t, nil)
	serve := func(path string, header ...string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		for i := 0; i < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		h.ServeHTTP(rec, req)
		return rec
	}

	for _, p := range []string{"/fonts/", "/fonts", "/finnsinte.js", "/main.go"} {
		if rec := serve(p); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, vill ha 404 (ingen katalogvisning)", p, rec.Code)
		}
	}
	// Routern städar sökvägen och skickar vidare till /main.go, som ger 404.
	if rec := serve("/../main.go"); rec.Code == http.StatusOK {
		t.Error("/../main.go ska aldrig ge en fil")
	}

	rec := serve("/fonts/font.woff2")
	etag := rec.Header().Get("ETag")
	if rec.Code != http.StatusOK || etag == "" || !strings.Contains(rec.Header().Get("Cache-Control"), "max-age") {
		t.Fatalf("status %d, ETag %q, Cache-Control %q", rec.Code, etag, rec.Header().Get("Cache-Control"))
	}
	if rec := serve("/fonts/font.woff2", "If-None-Match", etag); rec.Code != http.StatusNotModified {
		t.Errorf("oförändrad fil ska ge 304, fick %d", rec.Code)
	}
	if rec := serve("/"); rec.Header().Get("Cache-Control") != "no-cache" || rec.Header().Get("ETag") == "" {
		t.Errorf("index.html ska alltid kontrolleras: %v", rec.Header())
	}
}

func TestHealthz(t *testing.T) {
	serve := func(h http.Handler) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		return rec
	}

	// Före första hämtningen: servern lever, men listan saknas ännu.
	h, _ := newTestServer(t, nil)
	rec := serve(h)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "ok") || !strings.Contains(rec.Body.String(), "väntar") {
		t.Errorf("före hämtning: %d %q", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("healthz får inte cachas: %q", rec.Header().Get("Cache-Control"))
	}

	// Efter en sökning är listan hämtad.
	get[searchResponse](t, h, "/api/search", "Lund")
	if rec := serve(h); rec.Body.String() != "ok" {
		t.Errorf("efter hämtning: %q", rec.Body)
	}

	// Inaktuell lista ger fortfarande 200 men säger det.
	mock, _ := tnumock.New()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	idx := staleIndex{places.NewIndex(mock, 5*time.Minute, log)}
	srv, err := New(idx, fstest.MapFS{}, log)
	if err != nil {
		t.Fatal(err)
	}
	get[searchResponse](t, srv.Handler(), "/api/search", "Lund")
	rec = serve(srv.Handler())
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "inaktuell") {
		t.Errorf("inaktuell: %d %q", rec.Code, rec.Body)
	}
}

func TestAPICacheControl(t *testing.T) {
	h, _ := newTestServer(t, nil)
	for path, want := range map[string]string{"/api/search": cacheSearch, "/api/suggest": cacheSuggest} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path+"?q=Lund", nil))
		if got := rec.Header().Get("Cache-Control"); got != want {
			t.Errorf("%s: Cache-Control %q, vill ha %q", path, got, want)
		}
	}
}

func TestSearchReturnsSourceAndMeasuredAt(t *testing.T) {
	h, _ := newTestServer(t, nil)
	_, body := get[searchResponse](t, h, "/api/search", "Lund")
	if len(body.Stations) == 0 || body.Stations[0].Source == "" || body.Stations[0].MeasuredAt.IsZero() {
		t.Errorf("källa och mättid ska följa med: %+v", body.Stations[0])
	}
}

func TestValidationMessageUsesLimits(t *testing.T) {
	h, _ := newTestServer(t, nil)
	_, body := get[map[string]string](t, h, "/api/search", "a")
	if want := "Skriv mellan 2 och 50 tecken."; body["error"] != want {
		t.Errorf("felmeddelande %q, vill ha %q", body["error"], want)
	}
}
