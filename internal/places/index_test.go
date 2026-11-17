package places

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"temperatur/internal/obs"
)

// Schemaläggningen (intervall, omförsök, inaktuell data) testas i package
// feed; här testas bara att Index bygger en sökbar lista av källans svar.

type stubSource struct {
	stations []obs.Station
	err      error
}

func (s stubSource) AllStations(context.Context) ([]obs.Station, error) { return s.stations, s.err }

func newStubIndex(src obs.Source) *Index {
	return NewIndex(src, 5*time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestIndexBuildsSearchableSnapshot(t *testing.T) {
	x := newStubIndex(stubSource{stations: []obs.Station{
		{ID: "a", Title: "Alby", Kommun: "Botkyrka kommun"},
	}})
	if x.Ready() {
		t.Fatal("Ready ska vara false före första hämtningen")
	}
	snap, err := x.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !x.Ready() || x.Stale() {
		t.Fatalf("Ready=%v Stale=%v efter lyckad hämtning", x.Ready(), x.Stale())
	}
	if snap.FetchedAt.IsZero() {
		t.Error("FetchedAt ska vara satt")
	}
	if res := snap.Search("Alby", 10); res.Place == nil || res.Total != 1 {
		t.Errorf("Alby hittades inte: %+v", res)
	}
}

func TestIndexPassesOnSourceErrors(t *testing.T) {
	boom := errors.New("boom")
	x := newStubIndex(stubSource{err: boom})
	if _, err := x.Get(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("err = %v, vill ha boom", err)
	}
	if x.Ready() {
		t.Fatal("Ready ska vara false när hämtningen misslyckats")
	}
}
