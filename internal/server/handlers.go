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

// Meddelanden till besökaren. De ska säga vad som hände och vad man kan göra.
const (
	msgBadCharacters = "Sökningen innehåller tecken som inte går att använda. Skriv bara namnet på en ort eller kommun."
	msgSourceBusy    = "Temperaturerna kan inte hämtas just nu. Försök igen om några minuter."
	msgSourceFailed  = "Kunde inte hämta temperaturerna från temperatur.nu. Försök igen om några minuter."
	msgInternalError = "Något gick fel på servern. Försök igen om en stund."
)

var msgBadLength = fmt.Sprintf("Skriv mellan %d och %d tecken.", minQueryLen, maxQueryLen)

// internalErrorJSON skickas när svaret inte går att koda som JSON. Den är
// skriven för hand, eftersom det är kodningen som har misslyckats.
const internalErrorJSON = `{"error":"` + msgInternalError + `"}` + "\n"

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q, problem := query(r)
	if problem != "" {
		s.writeError(w, http.StatusBadRequest, problem)
		return
	}
	s.respond(w, r, cacheSearch, func(snap *places.Snapshot) any {
		res := searchResponse{
			Query:        q,
			SearchResult: snap.Search(q, maxStations, s.feelsLike),
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
	q, problem := query(r)
	if problem != "" {
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
		s.writeText(w, "ok: väntar på första hämtningen")
	case s.index.Stale():
		s.writeText(w, "ok: listan kan vara inaktuell")
	default:
		s.writeText(w, "ok")
	}
}

// query läser sökfrågan. Är den inte godtagbar är problem ett meddelande till
// besökaren om varför. Styrtecken (t.ex. NUL) avvisas så att de inte når
// loggar eller annan kod.
func query(r *http.Request) (q, problem string) {
	q = strings.Join(strings.Fields(r.URL.Query().Get("q")), " ")
	if strings.IndexFunc(q, unicode.IsControl) >= 0 || !utf8.ValidString(q) {
		return "", msgBadCharacters
	}
	if n := utf8.RuneCountInString(q); n < minQueryLen || n > maxQueryLen {
		return q, msgBadLength
	}
	return q, ""
}

func (s *Server) respond(w http.ResponseWriter, r *http.Request, cacheControl string, answer func(*places.Snapshot) any) {
	snap, ok := s.snapshot(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", cacheControl)
	s.writeJSON(w, http.StatusOK, answer(snap))
}

// snapshot hämtar listan att svara från, eller skriver ett felsvar. Felet
// kan bara uppstå innan någon lista har hämtats, eftersom en hämtad lista
// alltid finns kvar. Statuskoderna följer vems felet är:
//   - 503 när servern själv avstår från att anropa källan för att inte
//     överskrida anropsgränsen. Det går över av sig självt.
//   - 502 när källan inte svarade eller svarade fel (även när klienten är
//     blockerad; orsaken står i loggen från feed).
//   - Inget svar alls när besökaren har gått, eftersom ingen tar emot det.
//
// Ett nytt försök görs tidigast ett intervall senare (se feed.Feed), därför
// ber meddelandena besökaren vänta några minuter.
func (s *Server) snapshot(w http.ResponseWriter, r *http.Request) (*places.Snapshot, bool) {
	snap, err := s.index.Get(r.Context())
	switch {
	case err == nil:
		return snap, true
	case errors.Is(err, obs.ErrBudgetExhausted):
		s.writeError(w, http.StatusServiceUnavailable, msgSourceBusy)
	case errors.Is(err, context.Canceled):
	default:
		s.writeError(w, http.StatusBadGateway, msgSourceFailed)
	}
	return nil, false
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		s.log.Error("kunde inte skapa JSON-svar", "err", err)
		w.Header().Del("Cache-Control")
		status, b = http.StatusInternalServerError, []byte(internalErrorJSON)
	} else {
		b = append(b, '\n')
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	s.write(w, b)
}

func (s *Server) writeText(w http.ResponseWriter, text string) {
	s.write(w, []byte(text))
}

func (s *Server) write(w http.ResponseWriter, b []byte) {
	if _, err := w.Write(b); err != nil {
		s.log.Debug("kunde inte skriva svaret", "err", err) // oftast att besökaren gått
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	s.writeJSON(w, status, map[string]string{"error": msg})
}
