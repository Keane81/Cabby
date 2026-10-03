package fleet

import (
	"context"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/Keane81/Cabby/backend/emulator/internal/cabber"
	"github.com/Keane81/Cabby/backend/emulator/internal/config"
	"github.com/Keane81/Cabby/backend/emulator/internal/route"
	"github.com/Keane81/Cabby/backend/emulator/internal/stats"
)

// Phase of the run, for the live line.
type Phase int32

const (
	RampUp Phase = iota
	Steady
	Stopping
	Finished
)

var phaseNames = [...]string{"ramp-up", "steady", "stopping", "finished"}

func (p Phase) String() string { return phaseNames[p] }

const (
	defaultShutdownTimeout  = 10 * time.Second
	defaultLogoutConcurrent = 256
	defaultMonitorEvery     = 100 * time.Millisecond
)

// Params is everything one run needs. The zero values of the optional fields mean the defaults.
type Params struct {
	Profile config.Profile
	API     cabber.API
	Stats   *stats.Counters
	Log     zerolog.Logger

	// Optional seams, for tests.
	Now                 func() time.Time
	Sleep               func(ctx context.Context, d time.Duration) error
	ShutdownTimeout     time.Duration
	LogoutConcurrency   int
	AuthConcurrency     int
	MonitorEvery        time.Duration
	OnScheduleTolerance time.Duration
}

// Result is what the run knows about itself once it is over.
type Result struct {
	Cabbers    int // cabbers this process owned
	StartedAt  time.Time
	FinishedAt time.Time
	// TimeToFull is the time until every cabber had begun to send or had given up; zero if the run
	// ended before that.
	TimeToFull time.Duration
	// AttemptsAtFull is the number of location sends made (accepted or not) by that moment, so
	// that the steady rate can be counted from there.
	AttemptsAtFull int64
	// StoppedAt is the moment the last cabber stopped sending.
	StoppedAt time.Time
	// StopTook is the time the logout phase took.
	StopTook time.Duration
}

// Fleet is a running park. Phase can be read while Run is working.
type Fleet struct {
	p     Params
	phase atomic.Int32
}

// New prepares a run.
func New(p Params) *Fleet {
	if p.Now == nil {
		p.Now = time.Now
	}
	if p.Sleep == nil {
		p.Sleep = sleep
	}
	if p.ShutdownTimeout == 0 {
		p.ShutdownTimeout = defaultShutdownTimeout
	}
	if p.LogoutConcurrency == 0 {
		p.LogoutConcurrency = defaultLogoutConcurrent
	}
	if p.AuthConcurrency == 0 {
		p.AuthConcurrency = AuthConcurrency
	}
	if p.MonitorEvery == 0 {
		p.MonitorEvery = defaultMonitorEvery
	}
	return &Fleet{p: p}
}

// Phase is the current phase of the run.
func (f *Fleet) Phase() Phase { return Phase(f.phase.Load()) }

// Run starts the cabbers on the ramp-up schedule and works until ctx is done or the profile's
// Duration has passed. It then stops sending at once and logs the cabbers out; force, when it is
// done, cuts the logout phase short (the second signal of the user).
func (f *Fleet) Run(ctx, force context.Context) Result {
	p := f.p
	owned := Indices(p.Profile.Cabbers, p.Profile.Instances, p.Profile.Instance)
	result := Result{Cabbers: len(owned), StartedAt: p.Now()}

	runCtx := ctx
	if p.Profile.Duration > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, p.Profile.Duration)
		defer cancel()
	}

	limiter := NewLimiter(p.AuthConcurrency)
	env := cabber.Env{
		API: p.API, Stats: p.Stats, Log: p.Log, Auth: limiter,
		Now: p.Now, Sleep: p.Sleep, Jitter: rand.Float64, OnScheduleTolerance: p.OnScheduleTolerance,
	}

	cabbers := make([]*cabber.Cabber, len(owned))
	var wg sync.WaitGroup
	for n, index := range owned {
		id := cabber.NewIdentity(p.Profile.RunID, p.Profile.Seed, index)
		walker := route.New(p.Profile.Area, p.Profile.Seed, index)
		c := cabber.New(id, walker, p.Profile.Interval, env)
		cabbers[n] = c
		startAt := StartAt(result.StartedAt, index, p.Profile.Cabbers, p.Profile.RampUp)
		phase := SendPhase(p.Profile.Seed, index, p.Profile.Interval)
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Run(runCtx, startAt, phase)
		}()
	}

	monitorDone := make(chan struct{})
	reached := make(chan fullMoment, 1)
	go f.monitor(int64(len(owned)), result.StartedAt, monitorDone, reached)

	wg.Wait()
	result.StoppedAt = p.Now()
	close(monitorDone)
	full := <-reached
	result.TimeToFull, result.AttemptsAtFull = full.after, full.attempts

	f.phase.Store(int32(Stopping))
	stopStarted := p.Now()
	f.logout(force, cabbers)
	result.StopTook = p.Now().Sub(stopStarted)

	f.phase.Store(int32(Finished))
	result.FinishedAt = p.Now()
	return result
}

// fullMoment is when the park became complete: how long it took and how many sends had been made.
type fullMoment struct {
	after    time.Duration
	attempts int64
}

// monitor moves the run from ramp-up to steady when every cabber has begun to send or given up,
// and reports that moment (zero values if it never came) once done is closed.
func (f *Fleet) monitor(total int64, start time.Time, done <-chan struct{}, out chan<- fullMoment) {
	var full fullMoment
	reached := false
	check := func() {
		if reached {
			return
		}
		c := f.p.Stats
		if c.Started.Load()+c.Failed.Load() >= total {
			reached = true
			full = fullMoment{after: f.p.Now().Sub(start), attempts: c.SentOK.Load() + c.SendFailedTotal()}
			f.phase.CompareAndSwap(int32(RampUp), int32(Steady))
		}
	}
	ticker := time.NewTicker(f.p.MonitorEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			check()
		case <-done:
			check()
			out <- full
			return
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
