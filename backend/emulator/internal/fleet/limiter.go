package fleet

import "context"

// AuthConcurrency is how many registrations and logins one process runs at once. The auth service
// derives two password hashes at a time and gives up on a call after 2 s, so a short queue is what
// keeps the onboarding from turning into a wave of timeouts (research.md R-03). It is a constant,
// not a parameter: Clarifications decided against a separate logins-per-second knob.
const AuthConcurrency = 8

// Limiter is a counting semaphore that gives up when the run is stopped.
type Limiter chan struct{}

// NewLimiter allows n holders at once.
func NewLimiter(n int) Limiter { return make(Limiter, n) }

// Acquire waits for a place and returns the function that gives it back.
func (l Limiter) Acquire(ctx context.Context) (func(), error) {
	select {
	case l <- struct{}{}:
		return func() { <-l }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
