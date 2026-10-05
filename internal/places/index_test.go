package places

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"temperatur/internal/tnu"
)

type fakeSource struct {
	calls   atomic.Int32
	err     error
	release chan struct{} // om satt väntar AllStations tills den stängs
}

func (f *fakeSource) AllStations(context.Context) ([]tnu.Station, error) {
	f.calls.Add(1)
	if f.release != nil {
		<-f.release
	}
	if f.err != nil {
		return nil, f.err
	}
	return []tnu.Station{{ID: "a", Title: "Alby", Kommun: "Botkyrka kommun"}}, nil
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func newTestIndex(src tnu.Source) (*Index, *clock) {
	c := &clock{t: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)}
	x := NewIndex(src, 5*time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	x.now = c.now
	return x, c
}

// fakeTimer ersätter time.After så att testet styr när Run vaknar.
type fakeTimer struct {
	mu    sync.Mutex
	waits []time.Duration
	ticks chan time.Time
}

func newFakeTimer(x *Index) *fakeTimer {
	f := &fakeTimer{ticks: make(chan time.Time)}
	x.after = func(d time.Duration) <-chan time.Time {
		f.mu.Lock()
		f.waits = append(f.waits, d)
		f.mu.Unlock()
		return f.ticks
	}
	return f
}

// waitForSleep väntar tills Run har lagt sig och väntat n gånger, och
// returnerar hur länge den senast ville vänta.
func (f *fakeTimer) waitForSleep(t *testing.T, n int) time.Duration {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		if len(f.waits) >= n {
			d := f.waits[n-1]
			f.mu.Unlock()
			return d
		}
		f.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("Run lade sig aldrig för väntan nr %d", n)
	return 0
}

func TestRunFetchesAtStartAndEveryInterval(t *testing.T) {
	src := &fakeSource{}
	x, clk := newTestIndex(src)
	timer := newFakeTimer(x)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go x.Run(ctx)

	// Direkt vid start, innan någon besökare kommit.
	if d := timer.waitForSleep(t, 1); d != 5*time.Minute {
		t.Fatalf("väntar %v till nästa hämtning, vill ha 5m", d)
	}
	if n := src.calls.Load(); n != 1 {
		t.Fatalf("hämtade %d gånger vid start, vill ha 1", n)
	}

	for i := 2; i <= 4; i++ {
		clk.add(5 * time.Minute)
		timer.ticks <- time.Time{}
		timer.waitForSleep(t, i)
		if n := src.calls.Load(); n != int32(i) {
			t.Fatalf("efter %d intervall: %d hämtningar, vill ha %d", i-1, n, i)
		}
	}
}

func TestRunKeepsIntervalAfterErrors(t *testing.T) {
	src := &fakeSource{err: errors.New("boom")}
	x, clk := newTestIndex(src)
	timer := newFakeTimer(x)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go x.Run(ctx)

	if d := timer.waitForSleep(t, 1); d != 5*time.Minute {
		t.Fatalf("efter fel väntar Run %v, vill ha hela intervallet 5m", d)
	}
	// En väckning för tidigt (t.ex. klockjustering) ska inte ge nytt anrop.
	clk.add(time.Minute)
	timer.ticks <- time.Time{}
	if d := timer.waitForSleep(t, 2); d != 4*time.Minute {
		t.Fatalf("väntar %v, vill ha återstående 4m", d)
	}
	if n := src.calls.Load(); n != 1 {
		t.Fatalf("%d anrop inom intervallet, vill ha 1", n)
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	x, _ := newTestIndex(&fakeSource{})
	timer := newFakeTimer(x)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { x.Run(ctx); close(stopped) }()
	timer.waitForSleep(t, 1)
	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Run avslutades inte")
	}
}

func TestGetNeverWaitsOnceLoaded(t *testing.T) {
	src := &fakeSource{}
	x, clk := newTestIndex(src)
	ctx := context.Background()
	if _, err := x.Get(ctx); err != nil {
		t.Fatal(err)
	}

	// Även när listan är gammal svarar Get direkt; förnyelsen är Runs jobb.
	src.release = make(chan struct{}) // en hämtning skulle nu hänga
	clk.add(time.Hour)
	for range 10 {
		if snap, err := x.Get(ctx); err != nil || snap == nil {
			t.Fatalf("Get: %v", err)
		}
	}
	if n := src.calls.Load(); n != 1 {
		t.Fatalf("Get startade %d hämtningar, vill ha 1 (den första)", n)
	}
}

func TestIndexConcurrentFirstCallsShareOneFetch(t *testing.T) {
	src := &fakeSource{release: make(chan struct{})}
	x, _ := newTestIndex(src)

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if _, err := x.Get(context.Background()); err != nil {
				t.Error(err)
			}
		})
	}
	time.Sleep(20 * time.Millisecond)
	close(src.release)
	wg.Wait()
	if n := src.calls.Load(); n != 1 {
		t.Fatalf("hämtade %d gånger, vill ha 1", n)
	}
}

func TestIndexDoesNotRetryErrorsWithinInterval(t *testing.T) {
	boom := errors.New("boom")
	src := &fakeSource{err: boom}
	x, clk := newTestIndex(src)
	ctx := context.Background()

	for range 5 {
		if _, err := x.Get(ctx); !errors.Is(err, boom) {
			t.Fatalf("err = %v, vill ha boom", err)
		}
	}
	if n := src.calls.Load(); n != 1 {
		t.Fatalf("misslyckad hämtning försöktes %d gånger inom intervallet", n)
	}

	clk.add(5 * time.Minute)
	src.err = nil
	if _, err := x.Get(ctx); err != nil {
		t.Fatalf("efter intervallet ska nytt försök göras: %v", err)
	}
}

func TestIndexKeepsOldListWhenRefreshFails(t *testing.T) {
	src := &fakeSource{}
	x, clk := newTestIndex(src)
	ctx := context.Background()
	x.Get(ctx)

	src.err = errors.New("boom")
	clk.add(5 * time.Minute)
	<-x.refreshIfDue()
	snap, err := x.Get(ctx)
	if err != nil || snap == nil || len(snap.stations) != 1 {
		t.Fatalf("gamla listan ska finnas kvar: %v %v", snap, err)
	}
	if src.calls.Load() != 2 {
		t.Fatal("förnyelsen kördes inte")
	}
}

func TestStaleAfterMoreThanThreeFailuresInARow(t *testing.T) {
	src := &fakeSource{}
	x, clk := newTestIndex(src)
	ctx := context.Background()
	x.Get(ctx)
	first, _ := x.Get(ctx)

	refresh := func() {
		clk.add(5 * time.Minute)
		<-x.refreshIfDue()
	}

	src.err = errors.New("boom")
	for i := 1; i <= StaleAfterFailures; i++ {
		refresh()
		if x.Stale() {
			t.Fatalf("inaktuell redan efter %d misslyckanden", i)
		}
	}
	refresh()
	if !x.Stale() {
		t.Fatalf("ska vara inaktuell efter %d misslyckanden i rad", StaleAfterFailures+1)
	}
	if snap, err := x.Get(ctx); err != nil || snap != first {
		t.Fatalf("den gamla listan ska ligga kvar orörd: %v", err)
	}

	src.err = nil
	refresh()
	if x.Stale() {
		t.Fatal("ska inte vara inaktuell efter en lyckad hämtning")
	}
	if snap, _ := x.Get(ctx); snap == first {
		t.Fatal("den nya listan ska ha ersatt den gamla")
	}

	// Räkningen börjar om: tre nya fel räcker inte.
	src.err = errors.New("boom")
	for range StaleAfterFailures {
		refresh()
	}
	if x.Stale() {
		t.Fatal("felräkningen ska ha nollställts av den lyckade hämtningen")
	}
}
