package cabber

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/Keane81/Cabby/backend/emulator/internal/gateway"
	"github.com/Keane81/Cabby/backend/emulator/internal/route"
	"github.com/Keane81/Cabby/backend/emulator/internal/stats"
)

// State is the stage of the cabber's life (data-model.md, Cabber).
type State int

const (
	Waiting State = iota
	Registering
	LoggingIn
	Active
	Stopped
	LoggingOut
	Done
	Failed
	Lost
)

var stateNames = [...]string{"waiting", "registering", "logging_in", "active", "stopped", "logging_out", "done", "failed", "lost"}

func (s State) String() string { return stateNames[s] }

const (
	// maxAttempts bounds registration, login and re-login: a recovering system gets a few chances,
	// a dead one does not get a storm (research.md R-04).
	maxAttempts = 3
	// DefaultOnScheduleTolerance is how late a cabber may begin and still count as on schedule.
	DefaultOnScheduleTolerance = time.Second
	baseBackoff                = time.Second
)

// API is the part of the gateway client the cabber needs.
type API interface {
	Register(ctx context.Context, name, email, password string) gateway.Kind
	Login(ctx context.Context, email, password string) (string, gateway.Kind)
	RecordLocation(ctx context.Context, token string, latitude, longitude float64) gateway.Kind
	Logout(ctx context.Context, token string) gateway.Kind
}

// Limiter bounds how many registrations and logins run at once. A nil Limiter lets all through.
type Limiter interface {
	Acquire(ctx context.Context) (release func(), err error)
}

// Env is everything a cabber takes from outside, so that tests can replace time and the network.
type Env struct {
	API   API
	Stats *stats.Counters
	Log   zerolog.Logger
	Auth  Limiter
	// Now is the clock; Sleep waits until ctx is done or d has passed and returns ctx.Err() in the
	// first case. Jitter returns a number in [0, 1).
	// OnScheduleTolerance defaults to DefaultOnScheduleTolerance.
	OnScheduleTolerance time.Duration
	Now                 func() time.Time
	Sleep               func(ctx context.Context, d time.Duration) error
	Jitter              func() float64
}

// Cabber is one emulated cabber. One goroutine runs it; Token is read after Run has returned.
type Cabber struct {
	id       Identity
	walker   *route.Walker
	interval time.Duration
	env      Env

	state   State
	email   string
	token   string
	startAt time.Time
	began   bool // the first registration request has been counted against the schedule
}

// New builds the cabber with the given identity and walk.
func New(id Identity, walker *route.Walker, interval time.Duration, env Env) *Cabber {
	return &Cabber{id: id, walker: walker, interval: interval, env: env, state: Waiting}
}

// State is the final or current stage; read it only after Run has returned.
func (c *Cabber) State() State { return c.state }

// Token is the bearer token of the last session, empty if there is none.
func (c *Cabber) Token() string { return c.token }

// Run lives the cabber's life: wait for startAt, register and log in, then send positions on a
// grid whose first moment is phase after the login. It returns when ctx is done or when the cabber
// can no longer work; the session is left open for Logout.
func (c *Cabber) Run(ctx context.Context, startAt time.Time, phase time.Duration) {
	if err := c.env.Sleep(ctx, startAt.Sub(c.env.Now())); err != nil {
		c.state = Stopped
		return
	}
	c.startAt = startAt

	if !c.onboard(ctx) {
		return
	}
	c.env.Stats.Started.Add(1)
	c.drive(ctx, phase)
}

// onboard registers and logs in. It reports whether the cabber is ready to send.
func (c *Cabber) onboard(ctx context.Context) bool {
	c.state = Registering
	if !c.register(ctx) {
		return false
	}
	c.state = LoggingIn
	if !c.login(ctx, false) {
		return false
	}
	return true
}

func (c *Cabber) register(ctx context.Context) bool {
	attempt := 0 // the email variant
	for try := 1; ; try++ {
		email := c.id.Email(attempt)
		kind := c.limited(ctx, func() gateway.Kind {
			return c.env.API.Register(ctx, c.id.Name, email, c.id.Password)
		})
		switch {
		case kind == gateway.OK:
			c.email = email
			c.env.Stats.Registered.Add(1)
			return true
		case kind == gateway.Canceled:
			c.state = Stopped
			return false
		case kind == gateway.Conflict && attempt == 0:
			attempt = 1 // the address is taken: one more try under a different suffix
			try--
			continue
		case kind.Retryable() && try < maxAttempts:
			if !c.pause(ctx, try) {
				return false
			}
		default:
			c.env.Stats.RegisterFailed.Add(1)
			c.env.Stats.Failed.Add(1)
			c.state = Failed
			c.env.Log.Debug().Str("operation", "register").Str("error_class", kind.String()).Msg("cabber failed")
			return false
		}
	}
}

// login creates a session. relogin marks a new login after a lost session: it counts separately
// and ends in Lost instead of Failed.
func (c *Cabber) login(ctx context.Context, relogin bool) bool {
	for try := 1; ; try++ {
		var token string
		kind := c.limited(ctx, func() gateway.Kind {
			var k gateway.Kind
			token, k = c.env.API.Login(ctx, c.email, c.id.Password)
			return k
		})
		switch {
		case kind == gateway.OK:
			c.token = token
			if relogin {
				c.env.Stats.Relogin.Add(1)
			} else {
				c.env.Stats.LoggedIn.Add(1)
			}
			return true
		case kind == gateway.Canceled:
			c.state = Stopped
			return false
		case kind.Retryable() && try < maxAttempts:
			if !c.pause(ctx, try) {
				return false
			}
		default:
			c.env.Stats.LoginFailed.Add(1)
			if relogin {
				c.env.Stats.Lost.Add(1)
				c.state = Lost
			} else {
				c.env.Stats.Failed.Add(1)
				c.state = Failed
			}
			c.env.Log.Debug().Str("operation", "login").Str("error_class", kind.String()).Msg("cabber failed")
			return false
		}
	}
}

// limited runs one registration or login attempt inside the limiter and records its latency.
func (c *Cabber) limited(ctx context.Context, call func() gateway.Kind) gateway.Kind {
	if c.env.Auth != nil {
		release, err := c.env.Auth.Acquire(ctx)
		if err != nil {
			return gateway.Canceled
		}
		defer release()
	}
	started := c.env.Now()
	if !c.began {
		// The moment the first request leaves is when the cabber really begins: a queue in front of
		// the limiter, not the wake-up, is what makes a cabber late when auth cannot keep up.
		c.began = true
		tolerance := c.env.OnScheduleTolerance
		if tolerance == 0 {
			tolerance = DefaultOnScheduleTolerance
		}
		if started.Sub(c.startAt) <= tolerance {
			c.env.Stats.OnSchedule.Add(1)
		}
	}
	kind := call()
	if kind != gateway.Canceled {
		c.env.Stats.AuthLatency.Record(c.env.Now().Sub(started))
	}
	return kind
}

// pause waits before the next attempt: 1 s, 2 s, … with ±25% jitter. It reports false if the run
// was stopped while waiting.
func (c *Cabber) pause(ctx context.Context, try int) bool {
	d := baseBackoff << (try - 1)
	d = time.Duration(float64(d) * (0.75 + 0.5*c.env.Jitter()))
	if err := c.env.Sleep(ctx, d); err != nil {
		c.state = Stopped
		return false
	}
	return true
}

// drive sends positions until ctx is done. The grid of moments is anchored at the login plus the
// phase; a send that is late never makes up for the lost moments with a burst (R-04).
func (c *Cabber) drive(ctx context.Context, phase time.Duration) {
	c.state = Active
	c.env.Stats.Active.Add(1)

	slot := c.env.Now().Add(phase)
	lastStep := c.env.Now()
	for {
		if err := c.env.Sleep(ctx, slot.Sub(c.env.Now())); err != nil {
			c.env.Stats.Active.Add(-1)
			c.state = Stopped
			return
		}
		dispatched := c.env.Now()
		c.env.Stats.DispatchLag.Record(dispatched.Sub(slot))

		lat, lon := c.walker.Step(dispatched.Sub(lastStep))
		lastStep = dispatched
		kind := c.env.API.RecordLocation(ctx, c.token, lat, lon)

		switch kind {
		case gateway.OK:
			c.env.Stats.SentOK.Add(1)
			c.env.Stats.SendLatency.Record(c.env.Now().Sub(dispatched))
		case gateway.Canceled:
			c.env.Stats.Active.Add(-1)
			c.state = Stopped
			return
		case gateway.Unauthorized:
			c.env.Stats.SendFailed(kind)
			c.env.Stats.Active.Add(-1)
			c.state = LoggingIn
			if !c.login(ctx, true) {
				return
			}
			c.state = Active
			c.env.Stats.Active.Add(1)
		default:
			c.env.Stats.SendFailed(kind)
			c.env.Stats.SendLatency.Record(c.env.Now().Sub(dispatched))
			if kind == gateway.InvalidRequest {
				c.env.Log.Warn().Str("operation", "location").Str("error_class", kind.String()).Msg("emulator defect")
			}
		}
		slot = c.nextSlot(slot)
	}
}

// nextSlot is the next moment of the grid after slot that is still in the future; the moments
// already past are counted as skipped.
func (c *Cabber) nextSlot(slot time.Time) time.Time {
	next := slot.Add(c.interval)
	if now := c.env.Now(); !next.After(now) {
		missed := int64(now.Sub(next)/c.interval) + 1
		c.env.Stats.Skipped.Add(missed)
		next = next.Add(time.Duration(missed) * c.interval)
	}
	return next
}

// Logout revokes the session if there is one. It reports whether a session existed.
func (c *Cabber) Logout(ctx context.Context) (hadSession bool, kind gateway.Kind) {
	if c.token == "" {
		return false, gateway.OK
	}
	c.state = LoggingOut
	kind = c.env.API.Logout(ctx, c.token)
	switch kind {
	case gateway.OK:
		c.env.Stats.LoggedOut.Add(1)
		c.token = ""
		c.state = Done
	case gateway.Unauthorized:
		// Already revoked or expired: nothing is left to revoke.
		c.token = ""
		c.state = Done
	case gateway.Canceled:
		c.env.Stats.NotLoggedOut.Add(1)
	default:
		c.env.Stats.LogoutFailed.Add(1)
		c.env.Stats.NotLoggedOut.Add(1)
	}
	return true, kind
}
