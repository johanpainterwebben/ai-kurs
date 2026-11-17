package feed

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeSource ger som värde numret på anropet, så att testerna ser vilken
// hämtning ett värde kommer från.
type fakeSource struct {
	calls   atomic.Int32
	err     error
	release chan struct{} // om satt väntar fetch tills den stängs
}

func (f *fakeSource) fetch(context.Context) (int, error) {
	n := int(f.calls.Add(1))
	if f.release != nil {
		<-f.release
	}
	if f.err != nil {
		return 0, f.err
	}
	return n, nil
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func newTestFeed(fetch func(context.Context) (int, error)) (*Feed[int], *clock) {
	c := &clock{t: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)}
	f := New("test", 5*time.Minute, fetch, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.now = c.now
	return f, c
}

// fakeTimer ersätter time.After så att testet styr när Run vaknar.
type fakeTimer struct {
	mu    sync.Mutex
	waits []time.Duration
	ticks chan time.Time
}

func newFakeTimer[T any](f *Feed[T]) *fakeTimer {
	ft := &fakeTimer{ticks: make(chan time.Time)}
	f.after = func(d time.Duration) <-chan time.Time {
		ft.mu.Lock()
		ft.waits = append(ft.waits, d)
		ft.mu.Unlock()
		return ft.ticks
	}
	return ft
}

// waitForSleep väntar tills Run har lagt sig och väntat n gånger, och
// returnerar hur länge den senast ville vänta.
func (ft *fakeTimer) waitForSleep(t *testing.T, n int) time.Duration {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ft.mu.Lock()
		if len(ft.waits) >= n {
			d := ft.waits[n-1]
			ft.mu.Unlock()
			return d
		}
		ft.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("Run lade sig aldrig för väntan nr %d", n)
	return 0
}

func TestRunFetchesAtStartAndEveryInterval(t *testing.T) {
	src := &fakeSource{}
	f, clk := newTestFeed(src.fetch)
	timer := newFakeTimer(f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.Run(ctx)

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
	f, clk := newTestFeed(src.fetch)
	timer := newFakeTimer(f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.Run(ctx)

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
	f, _ := newTestFeed((&fakeSource{}).fetch)
	timer := newFakeTimer(f)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { f.Run(ctx); close(stopped) }()
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
	f, clk := newTestFeed(src.fetch)
	ctx := context.Background()
	if _, err := f.Get(ctx); err != nil {
		t.Fatal(err)
	}

	// Även när värdet är gammalt svarar Get direkt; förnyelsen är Runs jobb.
	src.release = make(chan struct{}) // en hämtning skulle nu hänga
	clk.add(time.Hour)
	for range 10 {
		if v, err := f.Get(ctx); err != nil || v != 1 {
			t.Fatalf("Get: %v, %v", v, err)
		}
	}
	if n := src.calls.Load(); n != 1 {
		t.Fatalf("Get startade %d hämtningar, vill ha 1 (den första)", n)
	}
}

func TestConcurrentFirstCallsShareOneFetch(t *testing.T) {
	src := &fakeSource{release: make(chan struct{})}
	f, _ := newTestFeed(src.fetch)

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if _, err := f.Get(context.Background()); err != nil {
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

func TestDoesNotRetryErrorsWithinInterval(t *testing.T) {
	boom := errors.New("boom")
	src := &fakeSource{err: boom}
	f, clk := newTestFeed(src.fetch)
	ctx := context.Background()

	for range 5 {
		if _, err := f.Get(ctx); !errors.Is(err, boom) {
			t.Fatalf("err = %v, vill ha boom", err)
		}
	}
	if n := src.calls.Load(); n != 1 {
		t.Fatalf("misslyckad hämtning försöktes %d gånger inom intervallet", n)
	}
	if f.Loaded() {
		t.Fatal("Loaded ska vara false när ingen hämtning lyckats")
	}

	clk.add(5 * time.Minute)
	src.err = nil
	if _, err := f.Get(ctx); err != nil {
		t.Fatalf("efter intervallet ska nytt försök göras: %v", err)
	}
	if !f.Loaded() {
		t.Fatal("Loaded ska vara true efter en lyckad hämtning")
	}
}

func TestKeepsOldValueWhenRefreshFails(t *testing.T) {
	src := &fakeSource{}
	f, clk := newTestFeed(src.fetch)
	ctx := context.Background()
	f.Get(ctx)

	src.err = errors.New("boom")
	clk.add(5 * time.Minute)
	<-f.refreshIfDue()
	v, err := f.Get(ctx)
	if err != nil || v != 1 {
		t.Fatalf("gamla värdet ska finnas kvar: %v %v", v, err)
	}
	if src.calls.Load() != 2 {
		t.Fatal("förnyelsen kördes inte")
	}
}

func TestStaleAfterMoreThanThreeFailuresInARow(t *testing.T) {
	src := &fakeSource{}
	f, clk := newTestFeed(src.fetch)
	ctx := context.Background()
	first, _ := f.Get(ctx)

	refresh := func() {
		clk.add(5 * time.Minute)
		<-f.refreshIfDue()
	}

	src.err = errors.New("boom")
	for i := 1; i <= StaleAfterFailures; i++ {
		refresh()
		if f.Stale() {
			t.Fatalf("inaktuell redan efter %d misslyckanden", i)
		}
	}
	refresh()
	if !f.Stale() {
		t.Fatalf("ska vara inaktuell efter %d misslyckanden i rad", StaleAfterFailures+1)
	}
	if v, err := f.Get(ctx); err != nil || v != first {
		t.Fatalf("det gamla värdet ska ligga kvar orört: %v", err)
	}

	src.err = nil
	refresh()
	if f.Stale() {
		t.Fatal("ska inte vara inaktuell efter en lyckad hämtning")
	}
	if v, _ := f.Get(ctx); v == first {
		t.Fatal("det nya värdet ska ha ersatt det gamla")
	}

	// Räkningen börjar om: tre nya fel räcker inte.
	src.err = errors.New("boom")
	for range StaleAfterFailures {
		refresh()
	}
	if f.Stale() {
		t.Fatal("felräkningen ska ha nollställts av den lyckade hämtningen")
	}
}

func TestPanicInFetchBecomesError(t *testing.T) {
	var calls atomic.Int32
	f, clk := newTestFeed(func(context.Context) (int, error) {
		calls.Add(1)
		panic("oväntad data")
	})
	if _, err := f.Get(context.Background()); err == nil || !strings.Contains(err.Error(), "panik") {
		t.Fatalf("panik ska bli ett fel, fick %v", err)
	}
	// Feed fungerar vidare och respekterar intervallet.
	f.Get(context.Background())
	if n := calls.Load(); n != 1 {
		t.Fatalf("%d försök inom intervallet, vill ha 1", n)
	}
	clk.add(5 * time.Minute)
	f.Get(context.Background())
	if n := calls.Load(); n != 2 {
		t.Fatalf("efter intervallet ska nytt försök göras, fick %d", n)
	}
}

// Två källor är oberoende: att den ena är nere påverkar inte den andra.
func TestFeedsAreIndependent(t *testing.T) {
	down := &fakeSource{err: errors.New("nere")}
	up := &fakeSource{}
	a, _ := newTestFeed(down.fetch)
	b, _ := newTestFeed(up.fetch)
	ctx := context.Background()

	if _, err := a.Get(ctx); err == nil {
		t.Fatal("källan som är nere ska ge fel")
	}
	if v, err := b.Get(ctx); err != nil || v != 1 {
		t.Fatalf("den fungerande källan påverkades: %v, %v", v, err)
	}
	if b.Stale() {
		t.Fatal("den fungerande källan ska inte vara inaktuell")
	}
}
