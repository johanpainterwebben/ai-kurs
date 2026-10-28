// Package budget räknar anrop mot en källas gräns per tidsfönster, t.ex. "högst
// 25 anrop per timme". Varje källa med en sådan gräns får en egen Budget.
package budget

import (
	"sync"
	"time"
)

// Budget tillåter högst limit anrop per rullande window.
type Budget struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu    sync.Mutex
	calls []time.Time // tidpunkter för anrop inom fönstret
}

// New skapar en budget. Är now nil används time.Now.
func New(limit int, window time.Duration, now func() time.Time) *Budget {
	if now == nil {
		now = time.Now
	}
	return &Budget{limit: limit, window: window, now: now}
}

// Take reserverar ett anrop om det finns utrymme i fönstret. Returnerar false
// om gränsen är nådd; då reserveras ingenting.
func (b *Budget) Take() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	cutoff := now.Add(-b.window)
	kept := b.calls[:0]
	for _, t := range b.calls {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	b.calls = kept
	if len(b.calls) >= b.limit {
		return false
	}
	b.calls = append(b.calls, now)
	return true
}
