// Package server exponerar sökningen som ett litet JSON-API och serverar webbsidan.
package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"temperatur/internal/places"
	"temperatur/internal/tnu"
)

const (
	minQueryLen    = 2
	maxQueryLen    = 50
	maxStations    = 100
	maxSuggestions = 8
	maxDidYouMean  = 3
)

type searchResponse struct {
	Query string `json:"query"`
	places.SearchResult
	FetchedAt  time.Time      `json:"fetchedAt"`  // när listan hämtades från temperatur.nu
	Stale      bool           `json:"stale"`      // flera hämtningar i rad har misslyckats
	DidYouMean []places.Place `json:"didYouMean"` // förslag när ingen plats hittades
}

type suggestResponse struct {
	Suggestions []places.Place `json:"suggestions"`
}

// Index är det servern behöver från places.Index.
type Index interface {
	Get(ctx context.Context) (*places.Snapshot, error)
	Stale() bool
}

// Server håller beroendena för HTTP-hanterarna.
type Server struct {
	index  Index
	static http.Handler
	log    *slog.Logger
}

// New skapar en server som serverar filerna i static som webbsida.
func New(index Index, static fs.FS, log *slog.Logger) (*Server, error) {
	h, err := staticHandler(static)
	if err != nil {
		return nil, err
	}
	return &Server{index: index, static: h, log: log}, nil
}

// Handler returnerar alla routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/suggest", s.handleSuggest)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.Handle("GET /", s.static)
	return securityHeaders(mux)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q, ok := query(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "Skriv mellan 2 och 50 tecken.")
		return
	}
	snap, ok := s.snapshot(w, r)
	if !ok {
		return
	}
	res := searchResponse{
		Query:        q,
		SearchResult: snap.Search(q, maxStations),
		FetchedAt:    snap.FetchedAt.UTC(),
		Stale:        s.index.Stale(),
		DidYouMean:   []places.Place{},
	}
	if res.Place == nil {
		res.DidYouMean = snap.Suggest(q, maxDidYouMean)
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSuggest(w http.ResponseWriter, r *http.Request) {
	q, ok := query(r)
	if !ok {
		writeJSON(w, http.StatusOK, suggestResponse{Suggestions: []places.Place{}})
		return
	}
	snap, ok := s.snapshot(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, suggestResponse{Suggestions: snap.Suggest(q, maxSuggestions)})
}

// query läser och normaliserar q-parametern och kontrollerar längden.
func query(r *http.Request) (string, bool) {
	q := strings.Join(strings.Fields(r.URL.Query().Get("q")), " ")
	n := utf8.RuneCountInString(q)
	return q, n >= minQueryLen && n <= maxQueryLen
}

// snapshot hämtar listan med mätpunkter och skriver ett felsvar om den saknas.
func (s *Server) snapshot(w http.ResponseWriter, r *http.Request) (*places.Snapshot, bool) {
	snap, err := s.index.Get(r.Context())
	switch {
	case err == nil:
		return snap, true
	case errors.Is(err, tnu.ErrBudgetExhausted):
		writeError(w, http.StatusServiceUnavailable, "Tjänsten är tillfälligt överbelastad. Försök igen om en stund.")
	case errors.Is(err, context.Canceled):
		// Besökaren gick innan svaret var klart.
	default:
		writeError(w, http.StatusBadGateway, "Kunde inte hämta data från temperatur.nu just nu.")
	}
	return nil, false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// staticHandler serverar de inbäddade filerna. Inbäddade filer saknar
// ändringsdatum, så utan ETag kan webbläsaren inte cacha dem och laddar ned
// allt vid varje besök. ETag är en hash av innehållet och räknas ut en gång;
// oförändrade filer ger då 304. Bara filer som finns serveras – inga
// kataloglistningar.
func staticHandler(files fs.FS) (http.Handler, error) {
	etags := map[string]string{}
	err := fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(files, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		etags["/"+p] = `"` + hex.EncodeToString(sum[:12]) + `"`
		return nil
	})
	if err != nil {
		return nil, err
	}

	fileServer := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/" {
			p = "/index.html"
		}
		etag, ok := etags[p]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", etag)
		switch path.Ext(p) {
		case ".html", ".css", ".js":
			// Fråga alltid om filen ändrats, så att en ny version syns direkt.
			w.Header().Set("Cache-Control", "no-cache")
		default:
			// Typsnitt och bilder ändras sällan.
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self'; style-src 'self'; script-src 'self'; "+
			"object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		// "Min plats" kommer att behöva geolocation=(self).
		h.Set("Permissions-Policy", "geolocation=(), camera=(), microphone=(), payment=(), usb=()")
		next.ServeHTTP(w, r)
	})
}
