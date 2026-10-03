package cabber

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/Keane81/Cabby/backend/emulator/internal/config"
	"github.com/Keane81/Cabby/backend/emulator/internal/gateway"
	"github.com/Keane81/Cabby/backend/emulator/internal/route"
	"github.com/Keane81/Cabby/backend/emulator/internal/stats"
)

// fakeClock is virtual time: Sleep advances it instantly, so a run of hours takes microseconds.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.sleeps = append(c.sleeps, d)
	if d > 0 {
		c.now = c.now.Add(d)
	}
	c.mu.Unlock()
	return nil
}

type sentPosition struct {
	at       time.Time
	token    string
	lat, lon float64
}

// fakeAPI answers from scripts; a nil script means success.
type fakeAPI struct {
	clock *fakeClock

	register func(call int, email string) gateway.Kind
	login    func(call int) (string, gateway.Kind)
	record   func(call int, p sentPosition) gateway.Kind
	logout   func(call int, token string) gateway.Kind

	mu            sync.Mutex
	registerCalls int
	loginCalls    int
	recordCalls   int
	logoutCalls   int
	emails        []string
	loginEmails   []string
	sent          []sentPosition
	order         []string
}

func (f *fakeAPI) Register(_ context.Context, _, email, _ string) gateway.Kind {
	f.mu.Lock()
	f.registerCalls++
	n := f.registerCalls
	f.emails = append(f.emails, email)
	f.order = append(f.order, "register")
	f.mu.Unlock()
	if f.register != nil {
		return f.register(n, email)
	}
	return gateway.OK
}

func (f *fakeAPI) Login(_ context.Context, email, _ string) (string, gateway.Kind) {
	f.mu.Lock()
	f.loginCalls++
	n := f.loginCalls
	f.loginEmails = append(f.loginEmails, email)
	f.order = append(f.order, "login")
	f.mu.Unlock()
	if f.login != nil {
		return f.login(n)
	}
	return "token-" + string(rune('0'+n)), gateway.OK
}

func (f *fakeAPI) RecordLocation(_ context.Context, token string, lat, lon float64) gateway.Kind {
	p := sentPosition{at: f.clock.Now(), token: token, lat: lat, lon: lon}
	f.mu.Lock()
	f.recordCalls++
	n := f.recordCalls
	f.sent = append(f.sent, p)
	f.order = append(f.order, "record")
	f.mu.Unlock()
	if f.record != nil {
		return f.record(n, p)
	}
	return gateway.OK
}

func (f *fakeAPI) Logout(_ context.Context, token string) gateway.Kind {
	f.mu.Lock()
	f.logoutCalls++
	n := f.logoutCalls
	f.order = append(f.order, "logout")
	f.mu.Unlock()
	if f.logout != nil {
		return f.logout(n, token)
	}
	return gateway.OK
}

type harness struct {
	clock  *fakeClock
	api    *fakeAPI
	stats  *stats.Counters
	cabber *Cabber
	ctx    context.Context
	cancel context.CancelFunc
}

const testInterval = 5 * time.Second

// newHarness builds a cabber on virtual time with a fixed jitter of 0.5, which makes every backoff
// exactly 1 s, 2 s, …
func newHarness(log zerolog.Logger, auth Limiter) *harness {
	clock := newFakeClock()
	api := &fakeAPI{clock: clock}
	counters := &stats.Counters{}
	env := Env{
		API: api, Stats: counters, Log: log, Auth: auth,
		Now: clock.Now, Sleep: clock.Sleep, Jitter: func() float64 { return 0.5 },
	}
	id := NewIdentity("run00001", 7, 3)
	walker := route.New(config.DefaultArea, 7, 3)
	ctx, cancel := context.WithCancel(context.Background())
	return &harness{clock: clock, api: api, stats: counters, cabber: New(id, walker, testInterval, env), ctx: ctx, cancel: cancel}
}

func quietHarness() *harness { return newHarness(zerolog.New(io.Discard), nil) }

// stopAfterSends cancels the run once n positions have been sent.
func (h *harness) stopAfterSends(n int) {
	previous := h.api.record
	h.api.record = func(call int, p sentPosition) gateway.Kind {
		kind := gateway.OK
		if previous != nil {
			kind = previous(call, p)
		}
		if call >= n {
			h.cancel()
		}
		return kind
	}
}

func (h *harness) run(phase time.Duration) {
	h.cabber.Run(h.ctx, h.clock.Now(), phase)
}

func (h *harness) ctxBackground() context.Context { return context.Background() }

func newHarnessWithLimiter(limiter Limiter) *harness {
	return newHarness(zerolog.New(io.Discard), limiter)
}
