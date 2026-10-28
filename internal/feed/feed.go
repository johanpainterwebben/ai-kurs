// Package feed hämtar data från en källa i bakgrunden och håller den senast
// hämtade i minnet.
//
// Besökare läser alltid ur den lokala kopian, så de väntar aldrig på källan.
// Varje källa får en egen Feed med eget intervall; om en källa är nere eller
// långsam påverkas inte de andra, och den senast lyckade hämtningen ligger
// kvar tills en ny har lyckats.
package feed

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// StaleAfterFailures är hur många misslyckade hämtningar i rad som tolereras
// innan datan räknas som inaktuell (se Stale).
const StaleAfterFailures = 3

// fetchTimeout är hur länge en enskild hämtning får ta.
const fetchTimeout = time.Minute

// Feed håller den senast hämtade datan av typen T. Run förnyar den i
// bakgrunden. Misslyckas en hämtning behålls den förra. Ett nytt försök görs
// aldrig tidigare än ett intervall efter det förra, inte heller efter fel –
// det är API-villkorens regel om hur ofta samma fråga får ställas.
type Feed[T any] struct {
	name     string
	fetch    func(context.Context) (T, error)
	interval time.Duration
	log      *slog.Logger
	now      func() time.Time
	after    func(time.Duration) <-chan time.Time

	mu          sync.Mutex
	value       T
	loaded      bool
	lastAttempt time.Time
	lastErr     error
	failures    int           // misslyckade hämtningar i rad
	refreshing  chan struct{} // icke-nil medan en hämtning pågår
}

// New skapar en Feed som anropar fetch högst en gång per interval. name
// används i loggarna.
func New[T any](name string, interval time.Duration, fetch func(context.Context) (T, error), log *slog.Logger) *Feed[T] {
	return &Feed[T]{name: name, fetch: fetch, interval: interval, log: log, now: time.Now, after: time.After}
}

// Run hämtar direkt och förnyar sedan en gång per intervall tills ctx
// avslutas. Körs i en egen gorutin.
func (f *Feed[T]) Run(ctx context.Context) {
	for {
		if done := f.refreshIfDue(); done != nil {
			select {
			case <-done:
			case <-ctx.Done():
				return
			}
		}
		f.mu.Lock()
		next := f.lastAttempt.Add(f.interval).Sub(f.now())
		f.mu.Unlock()
		select {
		case <-f.after(max(next, 0)):
		case <-ctx.Done():
			return
		}
	}
}

// Get returnerar senast hämtade värdet utan att vänta. Bara om inget värde
// har hunnit hämtas än (precis efter start) väntar Get på hämtningen.
func (f *Feed[T]) Get(ctx context.Context) (T, error) {
	f.mu.Lock()
	if f.loaded {
		v := f.value
		f.mu.Unlock()
		return v, nil
	}
	f.mu.Unlock()

	if wait := f.refreshIfDue(); wait != nil {
		select {
		case <-wait:
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loaded {
		return f.value, nil
	}
	var zero T
	return zero, f.lastErr
}

// Loaded anger om en hämtning har lyckats minst en gång.
func (f *Feed[T]) Loaded() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loaded
}

// Stale anger att fler än StaleAfterFailures hämtningar i rad har misslyckats,
// så att datan kan vara inaktuell. Nollställs vid nästa lyckade hämtning.
func (f *Feed[T]) Stale() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failures > StaleAfterFailures
}

// refreshIfDue startar en hämtning om ingen pågår och intervallet sedan
// förra försöket har gått. Returnerar den pågående hämtningens kanal, som
// stängs när den är klar, eller nil om ingen hämtning pågår.
func (f *Feed[T]) refreshIfDue() chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refreshing == nil && (f.lastAttempt.IsZero() || f.now().Sub(f.lastAttempt) >= f.interval) {
		f.lastAttempt = f.now()
		f.refreshing = make(chan struct{})
		go f.refresh(f.refreshing)
	}
	return f.refreshing
}

func (f *Feed[T]) refresh(done chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	start := f.now()
	v, err := f.safeFetch(ctx)
	if err == nil {
		f.log.Debug("hämtade", "källa", f.name, "tid", f.now().Sub(start))
	} else {
		f.log.Error("kunde inte hämta", "källa", f.name, "err", err)
	}

	f.mu.Lock()
	if err == nil {
		f.value, f.loaded = v, true // en misslyckad hämtning lämnar förra värdet orört
		if f.failures > StaleAfterFailures {
			f.log.Info("hämtningarna fungerar igen", "källa", f.name, "misslyckade_i_rad", f.failures)
		}
		f.failures = 0
	} else {
		f.failures++
		if f.failures == StaleAfterFailures+1 {
			f.log.Warn("datan räknas nu som inaktuell", "källa", f.name, "misslyckade_i_rad", f.failures)
		}
	}
	f.lastErr = err
	f.refreshing = nil
	f.mu.Unlock()
	close(done)
}

// safeFetch anropar fetch. En panik (t.ex. av oväntad data) blir ett vanligt
// fel: annars skulle servern krascha, startas om av Docker och hämta på nytt
// direkt – en loop som bryter mot femminutersregeln och kan få klienten
// blockerad.
func (f *Feed[T]) safeFetch(ctx context.Context) (v T, err error) {
	defer func() {
		if r := recover(); r != nil {
			var zero T
			v, err = zero, fmt.Errorf("feed %s: panik vid hämtning: %v", f.name, r)
		}
	}()
	return f.fetch(ctx)
}
