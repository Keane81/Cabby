package fleet

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/emulator/internal/stats"
)

func TestLimiterNeverLetsMoreThanItsSizeThrough(t *testing.T) {
	limiter := NewLimiter(8)
	var mu sync.Mutex
	inside, peak := 0, 0
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := limiter.Acquire(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			inside++
			if inside > peak {
				peak = inside
			}
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			inside--
			mu.Unlock()
			release()
		}()
	}
	wg.Wait()
	if peak > 8 {
		t.Errorf("%d holders at once, want at most 8", peak)
	}
	if peak < 2 {
		t.Errorf("peak %d: the test did not exercise concurrency", peak)
	}
}

func TestLimiterGivesUpWhenTheRunStops(t *testing.T) {
	limiter := NewLimiter(1)
	release, _ := limiter.Acquire(context.Background())
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := limiter.Acquire(ctx); err == nil {
		t.Fatal("acquired a place that was never free")
	}
}

func TestRampUpSpreadsTheRegistrations(t *testing.T) {
	api := newFakeAPI()
	counters := &stats.Counters{}
	const cabbers = 20
	ramp := 400 * time.Millisecond
	f := New(params(profile(cabbers, 50*time.Millisecond, ramp, 700*time.Millisecond), api, counters))
	result := f.Run(context.Background(), context.Background())

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.registers) != cabbers {
		t.Fatalf("%d registrations, want %d", len(api.registers), cabbers)
	}
	first, last := time.Now(), time.Time{}
	for _, at := range api.registers {
		if at.Before(first) {
			first = at
		}
		if at.After(last) {
			last = at
		}
	}
	if spread := last.Sub(first); spread < ramp*6/10 {
		t.Errorf("registrations spread over %s, want about the %s ramp-up (no burst)", spread, ramp)
	}
	s := counters.Snapshot()
	if s.Started != cabbers || s.OnSchedule < cabbers*8/10 {
		t.Errorf("started %d, on schedule %d of %d", s.Started, s.OnSchedule, cabbers)
	}
	if result.TimeToFull < ramp*7/10 || result.TimeToFull > 700*time.Millisecond {
		t.Errorf("time to full %s, want close to the %s ramp-up", result.TimeToFull, ramp)
	}
}

func TestAuthConcurrencyNeverExceedsTheLimit(t *testing.T) {
	api := newFakeAPI()
	api.authDelay = 10 * time.Millisecond
	counters := &stats.Counters{}
	p := params(profile(60, 100*time.Millisecond, 0, 600*time.Millisecond), api, counters) // ramp-up 0: everyone at once
	p.AuthConcurrency = 4
	New(p).Run(context.Background(), context.Background())
	if peak := api.peakAuth.Load(); peak > 4 {
		t.Errorf("%d registrations or logins at once, want at most 4", peak)
	}
}

func TestSlowAuthDelaysTheCabbersInsteadOfBursting(t *testing.T) {
	api := newFakeAPI()
	api.authDelay = 30 * time.Millisecond
	counters := &stats.Counters{}
	// 20 cabbers, two at a time, register + login 30 ms each: onboarding needs about 20*2*30/2 = 600 ms,
	// while the ramp-up asks for 100 ms.
	p := params(profile(20, 100*time.Millisecond, 100*time.Millisecond, 1500*time.Millisecond), api, counters)
	p.AuthConcurrency = 2
	p.OnScheduleTolerance = 50 * time.Millisecond
	result := New(p).Run(context.Background(), context.Background())

	if peak := api.peakAuth.Load(); peak > 2 {
		t.Errorf("%d at once, want at most 2", peak)
	}
	if result.TimeToFull < 400*time.Millisecond {
		t.Errorf("time to full %s: the park cannot have been ready that fast", result.TimeToFull)
	}
	s := counters.Snapshot()
	if s.OnSchedule >= 20 {
		t.Errorf("all %d cabbers claim to be on schedule although auth could not follow", s.OnSchedule)
	}
	if s.Started != 20 {
		t.Errorf("started %d, want all 20 eventually", s.Started)
	}
}

func TestPhaseMovesFromRampUpToSteadyToFinished(t *testing.T) {
	api := newFakeAPI()
	f := New(params(profile(10, 40*time.Millisecond, 200*time.Millisecond, 500*time.Millisecond), api, &stats.Counters{}))
	if f.Phase() != RampUp {
		t.Fatalf("phase before the run = %s", f.Phase())
	}
	seen := map[Phase]bool{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.Run(context.Background(), context.Background())
	}()
	for running := true; running; {
		select {
		case <-done:
			running = false
		default:
			seen[f.Phase()] = true
			time.Sleep(time.Millisecond)
		}
	}
	seen[f.Phase()] = true
	for _, want := range []Phase{RampUp, Steady, Finished} {
		if !seen[want] {
			t.Errorf("phase %s was never observed (saw %v)", want, seen)
		}
	}
}
