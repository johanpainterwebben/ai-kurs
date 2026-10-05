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

	"temperatur/internal/places"
	"temperatur/internal/tnu"
)

type countingSource struct {
	tnu.Source
	calls atomic.Int32
	err   error
}

func (c *countingSource) AllStations(ctx context.Context) ([]tnu.Station, error) {
	c.calls.Add(1)
	if c.err != nil {
		return nil, c.err
	}
	return c.Source.AllStations(ctx)
}

func newTestServer(t *testing.T, err error) (http.Handler, *countingSource) {
	t.Helper()
	mock, e := tnu.NewMock()
	if e != nil {
		t.Fatal(e)
	}
	src := &countingSource{Source: mock, err: err}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	static := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}
	return New(places.NewIndex(src, 5*time.Minute, log), static, log).Handler(), src
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

	mock, _ := tnu.NewMock()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	idx := staleIndex{places.NewIndex(mock, 5*time.Minute, log)}
	h = New(idx, fstest.MapFS{}, log).Handler()
	code, body := get[searchResponse](t, h, "/api/search", "Lund")
	if code != http.StatusOK || !body.Stale || body.Total != 10 {
		t.Fatalf("status %d, stale=%v, total=%d; vill ha 200, true, 10", code, body.Stale, body.Total)
	}
}

func TestValidation(t *testing.T) {
	h, src := newTestServer(t, nil)
	for _, q := range []string{"", " a ", strings.Repeat("a", 51)} {
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
	h, _ := newTestServer(t, tnu.ErrBudgetExhausted)
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
