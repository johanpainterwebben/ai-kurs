// Package places håller en lokal kopia av alla mätpunkter och söker i den.
//
// Hela listan hämtas med ett enda API-anrop när servern startar och förnyas
// sedan i bakgrunden en gång per intervall (se package feed). Besökare läser
// alltid ur den lokala kopian, så de väntar aldrig på temperatur.nu och deras
// sökningar, förslag och felstavningar kostar inga API-anrop.
package places

import (
	"context"
	"log/slog"
	"time"

	"temperatur/internal/feed"
	"temperatur/internal/obs"
)

// Index håller den senast hämtade listan, förberedd för sökning.
type Index struct {
	feed *feed.Feed[*Snapshot]
}

// NewIndex skapar ett index som förnyas högst en gång per interval.
func NewIndex(src obs.Source, interval time.Duration, log *slog.Logger) *Index {
	fetch := func(ctx context.Context) (*Snapshot, error) {
		stations, err := src.AllStations(ctx)
		if err != nil {
			return nil, err
		}
		log.Info("hämtade alla mätpunkter", "antal", len(stations))
		return newSnapshot(stations, time.Now()), nil
	}
	return &Index{feed: feed.New("mätpunkter", interval, fetch, log)}
}

// Run hämtar listan direkt och förnyar den sedan en gång per intervall tills
// ctx avslutas. Körs i en egen gorutin.
func (x *Index) Run(ctx context.Context) { x.feed.Run(ctx) }

// Get returnerar aktuell lista utan att vänta. Bara om ingen lista har
// hunnit hämtas än (precis efter start) väntar Get på hämtningen.
func (x *Index) Get(ctx context.Context) (*Snapshot, error) { return x.feed.Get(ctx) }

// Stale anger att flera hämtningar i rad har misslyckats, så att listan kan
// vara inaktuell. Nollställs vid nästa lyckade hämtning.
func (x *Index) Stale() bool { return x.feed.Stale() }

// Ready anger om en lista har hämtats minst en gång.
func (x *Index) Ready() bool { return x.feed.Loaded() }
