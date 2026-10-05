// Package places håller en lokal kopia av alla mätpunkter och söker i den.
//
// Hela listan hämtas med ett enda API-anrop när servern startar och förnyas
// sedan i bakgrunden en gång per intervall. Besökare läser alltid ur den
// lokala kopian, så de väntar aldrig på temperatur.nu och deras sökningar,
// förslag och felstavningar kostar inga API-anrop.
package places

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"temperatur/internal/tnu"
)

// StaleAfterFailures är hur många misslyckade hämtningar i rad som tolereras
// innan listan räknas som inaktuell (se Stale).
const StaleAfterFailures = 3

// Index håller den senast hämtade listan. Run förnyar den i bakgrunden.
// Misslyckas en hämtning behålls den förra listan. Ett nytt försök görs
// aldrig tidigare än ett intervall efter det förra, inte heller efter fel –
// det är API-villkorets femminutersregel.
type Index struct {
	src      tnu.Source
	interval time.Duration
	log      *slog.Logger
	now      func() time.Time
	after    func(time.Duration) <-chan time.Time

	mu          sync.Mutex
	snap        *Snapshot
	lastAttempt time.Time
	lastErr     error
	failures    int           // misslyckade hämtningar i rad
	refreshing  chan struct{} // icke-nil medan en hämtning pågår
}

// NewIndex skapar ett index som förnyas högst en gång per interval.
func NewIndex(src tnu.Source, interval time.Duration, log *slog.Logger) *Index {
	return &Index{src: src, interval: interval, log: log, now: time.Now, after: time.After}
}

// Run hämtar listan direkt och förnyar den sedan en gång per intervall tills
// ctx avslutas. Körs i en egen gorutin.
func (x *Index) Run(ctx context.Context) {
	for {
		if done := x.refreshIfDue(); done != nil {
			select {
			case <-done:
			case <-ctx.Done():
				return
			}
		}
		x.mu.Lock()
		next := x.lastAttempt.Add(x.interval).Sub(x.now())
		x.mu.Unlock()
		select {
		case <-x.after(max(next, 0)):
		case <-ctx.Done():
			return
		}
	}
}

// Get returnerar aktuell lista utan att vänta. Bara om ingen lista har
// hunnit hämtas än (precis efter start) väntar Get på hämtningen.
func (x *Index) Get(ctx context.Context) (*Snapshot, error) {
	x.mu.Lock()
	snap := x.snap
	x.mu.Unlock()
	if snap != nil {
		return snap, nil
	}

	if wait := x.refreshIfDue(); wait != nil {
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	x.mu.Lock()
	defer x.mu.Unlock()
	if x.snap != nil {
		return x.snap, nil
	}
	return nil, x.lastErr
}

// Stale anger att fler än StaleAfterFailures hämtningar i rad har misslyckats,
// så att listan kan vara inaktuell. Nollställs vid nästa lyckade hämtning.
func (x *Index) Stale() bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.failures > StaleAfterFailures
}

// refreshIfDue startar en hämtning om ingen pågår och intervallet sedan
// förra försöket har gått. Returnerar den pågående hämtningens kanal, som
// stängs när den är klar, eller nil om ingen hämtning pågår.
func (x *Index) refreshIfDue() chan struct{} {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.refreshing == nil && (x.lastAttempt.IsZero() || x.now().Sub(x.lastAttempt) >= x.interval) {
		x.lastAttempt = x.now()
		x.refreshing = make(chan struct{})
		go x.refresh(x.refreshing)
	}
	return x.refreshing
}

func (x *Index) refresh(done chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	start := x.now()
	stations, err := x.src.AllStations(ctx)
	var snap *Snapshot
	if err == nil {
		snap = newSnapshot(stations, x.now())
		x.log.Info("hämtade alla mätpunkter", "antal", len(stations), "tid", x.now().Sub(start))
	} else {
		x.log.Error("kunde inte hämta mätpunkter", "err", err)
	}

	x.mu.Lock()
	if snap != nil {
		x.snap = snap // en misslyckad hämtning lämnar den förra listan orörd
		if x.failures > StaleAfterFailures {
			x.log.Info("hämtningarna fungerar igen", "misslyckade_i_rad", x.failures)
		}
		x.failures = 0
	} else {
		x.failures++
		if x.failures == StaleAfterFailures+1 {
			x.log.Warn("listan räknas nu som inaktuell", "misslyckade_i_rad", x.failures)
		}
	}
	x.lastErr = err
	x.refreshing = nil
	x.mu.Unlock()
	close(done)
}
