package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"temperatur/internal/obs"
	"temperatur/internal/places"
)

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q, ok := query(r)
	if !ok {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Skriv mellan %d och %d tecken.", minQueryLen, maxQueryLen))
		return
	}
	s.respond(w, r, cacheSearch, func(snap *places.Snapshot) any {
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
		return res
	})
}

func (s *Server) handleSuggest(w http.ResponseWriter, r *http.Request) {
	q, ok := query(r)
	if !ok {
		s.writeJSON(w, http.StatusOK, suggestResponse{Suggestions: []places.Place{}})
		return
	}
	s.respond(w, r, cacheSuggest, func(snap *places.Snapshot) any {
		return suggestResponse{Suggestions: snap.Suggest(q, maxSuggestions)}
	})
}

// handleHealth svarar 200 så länge servern kan svara; det är en kontroll av
// att processen lever, inte av att temperatur.nu fungerar. Texten säger om
// listan med mätpunkter ännu inte hämtats eller kan vara inaktuell.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	switch {
	case !s.index.Ready():
		w.Write([]byte("ok: väntar på första hämtningen"))
	case s.index.Stale():
		w.Write([]byte("ok: listan kan vara inaktuell"))
	default:
		w.Write([]byte("ok"))
	}
}

// query avvisar styrtecken (t.ex. NUL) så att de inte når loggar eller annan kod.
func query(r *http.Request) (string, bool) {
	q := strings.Join(strings.Fields(r.URL.Query().Get("q")), " ")
	if strings.IndexFunc(q, unicode.IsControl) >= 0 || !utf8.ValidString(q) {
		return "", false
	}
	n := utf8.RuneCountInString(q)
	return q, n >= minQueryLen && n <= maxQueryLen
}

func (s *Server) respond(w http.ResponseWriter, r *http.Request, cacheControl string, answer func(*places.Snapshot) any) {
	snap, ok := s.snapshot(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", cacheControl)
	s.writeJSON(w, http.StatusOK, answer(snap))
}

func (s *Server) snapshot(w http.ResponseWriter, r *http.Request) (*places.Snapshot, bool) {
	snap, err := s.index.Get(r.Context())
	switch {
	case err == nil:
		return snap, true
	case errors.Is(err, obs.ErrBudgetExhausted):
		s.writeError(w, http.StatusServiceUnavailable, "Tjänsten är tillfälligt överbelastad. Försök igen om en stund.")
	case errors.Is(err, context.Canceled):
		// Besökaren gick innan svaret var klart.
	default:
		s.writeError(w, http.StatusBadGateway, "Kunde inte hämta data från temperatur.nu just nu.")
	}
	return nil, false
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		s.log.Error("kunde inte skapa JSON-svar", "err", err)
		w.Header().Del("Cache-Control")
		http.Error(w, "Internt fel.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(append(b, '\n')); err != nil {
		s.log.Debug("kunde inte skriva svaret", "err", err) // oftast att besökaren gått
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	s.writeJSON(w, status, map[string]string{"error": msg})
}
