package budget

import (
	"testing"
	"time"
)

func TestTakeStopsAtLimitAndRecoversAfterWindow(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	b := New(3, time.Hour, func() time.Time { return now })

	for i := range 3 {
		if !b.Take() {
			t.Fatalf("anrop %d ska tillåtas", i+1)
		}
	}
	if b.Take() {
		t.Fatal("fjärde anropet inom fönstret ska stoppas")
	}

	now = now.Add(30 * time.Minute)
	if b.Take() {
		t.Fatal("anropen är fortfarande inom fönstret")
	}

	now = now.Add(30*time.Minute + time.Second)
	if !b.Take() {
		t.Fatal("efter fönstret ska anrop tillåtas igen")
	}
}

func TestRejectedTakeDoesNotUseBudget(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	b := New(1, time.Hour, func() time.Time { return now })
	b.Take()
	for range 10 {
		b.Take() // nekade anrop ska inte förlänga spärren
	}
	now = now.Add(time.Hour + time.Second)
	if !b.Take() {
		t.Fatal("nekade anrop får inte räknas")
	}
}
