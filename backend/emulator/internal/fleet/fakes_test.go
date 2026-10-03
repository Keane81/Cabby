package fleet

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/Keane81/Cabby/backend/emulator/internal/config"
	"github.com/Keane81/Cabby/backend/emulator/internal/gateway"
	"github.com/Keane81/Cabby/backend/emulator/internal/stats"
)

// fakeAPI is a gateway that answers after a configurable delay and keeps what the tests ask about.
type fakeAPI struct {
	authDelay     time.Duration
	locationDelay time.Duration
	logoutDelay   time.Duration

	inFlight    atomic.Int64
	peakAuth    atomic.Int64
	records     atomic.Int64
	logouts     atomic.Int64
	loginTokens atomic.Int64

	mu        sync.Mutex
	registers map[string]time.Time // email → when the registration reached the gateway
	order     []string
}

func newFakeAPI() *fakeAPI { return &fakeAPI{registers: map[string]time.Time{}} }

func (f *fakeAPI) enterAuth() func() {
	n := f.inFlight.Add(1)
	for {
		peak := f.peakAuth.Load()
		if n <= peak || f.peakAuth.CompareAndSwap(peak, n) {
			break
		}
	}
	return func() { f.inFlight.Add(-1) }
}

func wait(ctx context.Context, d time.Duration) gateway.Kind {
	if d <= 0 {
		return gateway.OK
	}
	select {
	case <-time.After(d):
		return gateway.OK
	case <-ctx.Done():
		return gateway.Canceled
	}
}

func (f *fakeAPI) Register(ctx context.Context, _, email, _ string) gateway.Kind {
	defer f.enterAuth()()
	f.mu.Lock()
	f.registers[email] = time.Now()
	f.order = append(f.order, email)
	f.mu.Unlock()
	return wait(ctx, f.authDelay)
}

func (f *fakeAPI) Login(ctx context.Context, email, _ string) (string, gateway.Kind) {
	defer f.enterAuth()()
	if kind := wait(ctx, f.authDelay); kind != gateway.OK {
		return "", kind
	}
	return "token-" + email, gateway.OK
}

func (f *fakeAPI) RecordLocation(ctx context.Context, _ string, _, _ float64) gateway.Kind {
	f.records.Add(1)
	return wait(ctx, f.locationDelay)
}

func (f *fakeAPI) Logout(ctx context.Context, _ string) gateway.Kind {
	kind := wait(ctx, f.logoutDelay)
	if kind == gateway.OK {
		f.logouts.Add(1)
	}
	return kind
}

// profile is a small, fast profile for real-time tests.
func profile(cabbers int, interval, rampUp, duration time.Duration) config.Profile {
	p := config.Default()
	p.Cabbers = cabbers
	p.Interval = interval
	p.RampUp = rampUp
	p.Duration = duration
	p.RunID = "testrun1"
	p.Seed = 11
	return p
}

func params(p config.Profile, api *fakeAPI, counters *stats.Counters) Params {
	return Params{
		Profile: p, API: api, Stats: counters, Log: zerolog.New(io.Discard),
		MonitorEvery: 5 * time.Millisecond,
	}
}
