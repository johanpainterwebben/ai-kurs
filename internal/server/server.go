// Package server exponerar sökningen som ett litet JSON-API och serverar webbsidan.
package server

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"temperatur/internal/places"
)

// Gränser för frågor och svar.
const (
	minQueryLen    = 2
	maxQueryLen    = 50
	maxStations    = 100
	maxSuggestions = 8
	maxDidYouMean  = 3
)

// Cache-Control för API-svaren. Svaren bygger på en lista som förnyas var
// tionde minut, så några minuters cache hos besökare och Cloudflare är säkert.
const (
	cacheSearch  = "public, max-age=60"
	cacheSuggest = "public, max-age=300"
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
	Ready() bool
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
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.Handle("GET /", s.static)
	return securityHeaders(mux)
}
