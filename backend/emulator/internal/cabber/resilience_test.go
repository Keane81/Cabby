package cabber

import (
	"context"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/emulator/internal/gateway"
)

func TestFailedSendIsNeverRetried(t *testing.T) {
	h := quietHarness()
	h.api.record = func(call int, p sentPosition) gateway.Kind {
		if call == 2 {
			return gateway.Unavailable
		}
		return gateway.OK
	}
	h.stopAfterSends(4)
	h.run(0)

	// One call per slot: the failed second send is followed by the third one a full interval
	// later, not by an immediate second attempt.
	if h.api.recordCalls != 4 {
		t.Fatalf("%d calls, want 4 (one per slot, no retry)", h.api.recordCalls)
	}
	if d := h.api.sent[2].at.Sub(h.api.sent[1].at); d != testInterval {
		t.Errorf("after the failure the next send came %s later, want %s", d, testInterval)
	}
	s := h.stats.Snapshot()
	if s.SentOK != 3 || s.SendFailed["unavailable"] != 1 || s.Skipped != 0 {
		t.Errorf("stats = %+v", s)
	}
}

func TestEveryFailureKindIsCountedByKind(t *testing.T) {
	h := quietHarness()
	kinds := []gateway.Kind{gateway.Timeout, gateway.Network, gateway.Unavailable, gateway.Unexpected}
	h.api.record = func(call int, p sentPosition) gateway.Kind {
		if call <= len(kinds) {
			return kinds[call-1]
		}
		return gateway.OK
	}
	h.stopAfterSends(len(kinds) + 1)
	h.run(0)
	s := h.stats.Snapshot()
	for _, name := range []string{"timeout", "network", "unavailable", "unexpected"} {
		if s.SendFailed[name] != 1 {
			t.Errorf("%s = %d, want 1 (%v)", name, s.SendFailed[name], s.SendFailed)
		}
	}
}

func TestUnauthorizedLeadsToANewLoginAndSendingResumes(t *testing.T) {
	h := quietHarness()
	h.api.record = func(call int, p sentPosition) gateway.Kind {
		if call == 3 {
			return gateway.Unauthorized
		}
		return gateway.OK
	}
	h.stopAfterSends(6)
	h.run(0)

	if h.api.loginCalls != 2 || h.api.registerCalls != 1 {
		t.Fatalf("%d logins and %d registrations, want 2 and 1", h.api.loginCalls, h.api.registerCalls)
	}
	if last := h.api.sent[len(h.api.sent)-1]; last.token != "token-2" {
		t.Errorf("sending resumed with %q, want the token of the new session", last.token)
	}
	s := h.stats.Snapshot()
	if s.Relogin != 1 || s.SendFailed["unauthorized"] != 1 || s.Lost != 0 {
		t.Errorf("stats = %+v", s)
	}
	if h.api.loginEmails[0] != h.api.loginEmails[1] {
		t.Error("the re-login used another account")
	}
}

func TestRegistrationRetriesTransientFailuresWithGrowingPauses(t *testing.T) {
	h := quietHarness()
	h.api.register = func(call int, _ string) gateway.Kind {
		if call <= 2 {
			return gateway.Unavailable
		}
		return gateway.OK
	}
	h.stopAfterSends(1)
	h.run(0)

	if h.api.registerCalls != 3 {
		t.Fatalf("%d attempts, want 3", h.api.registerCalls)
	}
	pauses := h.clock.sleeps[1:3] // the first sleep is the wait for the start
	if pauses[0] != time.Second || pauses[1] != 2*time.Second {
		t.Errorf("pauses = %v, want 1s then 2s", pauses)
	}
	if h.stats.Snapshot().Registered != 1 {
		t.Error("the cabber did not end up registered")
	}
}

func TestBackoffJitterStaysWithinAQuarter(t *testing.T) {
	for _, jitter := range []float64{0, 0.5, 0.999} {
		h := quietHarness()
		h.cabber.env.Jitter = func() float64 { return jitter }
		h.api.register = func(call int, _ string) gateway.Kind {
			if call == 1 {
				return gateway.Timeout
			}
			return gateway.OK
		}
		h.stopAfterSends(1)
		h.run(0)
		pause := h.clock.sleeps[1]
		if pause < 750*time.Millisecond || pause > 1250*time.Millisecond {
			t.Errorf("jitter %v gave a pause of %s, want 0.75–1.25 s", jitter, pause)
		}
	}
}

func TestRegistrationGivesUpAfterThreeAttempts(t *testing.T) {
	h := quietHarness()
	h.api.register = func(int, string) gateway.Kind { return gateway.Unavailable }
	h.run(0)
	if h.api.registerCalls != 3 || h.api.loginCalls != 0 {
		t.Fatalf("%d attempts and %d logins", h.api.registerCalls, h.api.loginCalls)
	}
	s := h.stats.Snapshot()
	if h.cabber.State() != Failed || s.Failed != 1 || s.RegisterFailed != 1 || s.Started != 0 {
		t.Errorf("state %s, stats %+v", h.cabber.State(), s)
	}
}

func TestPermanentFailuresAreNotRetried(t *testing.T) {
	for _, kind := range []gateway.Kind{gateway.InvalidRequest, gateway.Unauthorized, gateway.Unexpected} {
		h := quietHarness()
		h.api.register = func(int, string) gateway.Kind { return kind }
		h.run(0)
		if h.api.registerCalls != 1 || h.cabber.State() != Failed {
			t.Errorf("%s: %d attempts, state %s", kind, h.api.registerCalls, h.cabber.State())
		}
	}
}

func TestConflictRetriesOnceUnderAnotherAddress(t *testing.T) {
	h := quietHarness()
	h.api.register = func(call int, _ string) gateway.Kind {
		if call == 1 {
			return gateway.Conflict
		}
		return gateway.OK
	}
	h.stopAfterSends(1)
	h.run(0)

	if h.api.registerCalls != 2 || h.api.emails[0] == h.api.emails[1] {
		t.Fatalf("registrations %v, want two different addresses", h.api.emails)
	}
	if h.api.loginEmails[0] != h.api.emails[1] {
		t.Errorf("logged in as %q, want the address that was registered", h.api.loginEmails[0])
	}
}

func TestSecondConflictFailsTheCabber(t *testing.T) {
	h := quietHarness()
	h.api.register = func(int, string) gateway.Kind { return gateway.Conflict }
	h.run(0)
	if h.api.registerCalls != 2 || h.cabber.State() != Failed {
		t.Fatalf("%d attempts, state %s", h.api.registerCalls, h.cabber.State())
	}
}

func TestLoginRetriesAndThenFails(t *testing.T) {
	h := quietHarness()
	h.api.login = func(int) (string, gateway.Kind) { return "", gateway.Network }
	h.run(0)
	s := h.stats.Snapshot()
	if h.api.loginCalls != 3 || h.cabber.State() != Failed || s.LoginFailed != 1 || s.Failed != 1 || s.Lost != 0 {
		t.Errorf("%d logins, state %s, stats %+v", h.api.loginCalls, h.cabber.State(), s)
	}
}

func TestThreeFailedRelogginsEndInLost(t *testing.T) {
	h := quietHarness()
	h.api.record = func(call int, p sentPosition) gateway.Kind { return gateway.Unauthorized }
	h.api.login = func(call int) (string, gateway.Kind) {
		if call == 1 {
			return "token-1", gateway.OK
		}
		return "", gateway.Unavailable
	}
	h.run(0)
	s := h.stats.Snapshot()
	if h.cabber.State() != Lost || s.Lost != 1 || s.Failed != 0 || s.Active != 0 {
		t.Fatalf("state %s, stats %+v", h.cabber.State(), s)
	}
	if h.api.loginCalls != 1+3 {
		t.Errorf("%d logins, want the first plus three re-login attempts", h.api.loginCalls)
	}
}

func TestSlowSendDropsTheMissedSlotsInsteadOfBursting(t *testing.T) {
	h := quietHarness()
	h.api.record = func(call int, p sentPosition) gateway.Kind {
		if call == 2 {
			h.clock.Advance(3*testInterval + testInterval/2) // a send lasting 3.5 intervals
		}
		return gateway.OK
	}
	h.stopAfterSends(4)
	h.run(0)

	s := h.stats.Snapshot()
	if s.Skipped != 3 {
		t.Errorf("skipped = %d, want the 3 slots that fell inside the slow send", s.Skipped)
	}
	// The send after the slow one lands on the grid, half an interval after the slow send ended.
	if d := h.api.sent[2].at.Sub(h.api.sent[1].at); d != 4*testInterval {
		t.Errorf("next send %s after the slow one started, want %s", d, 4*testInterval)
	}
	if h.api.sent[3].at.Sub(h.api.sent[2].at) != testInterval {
		t.Error("the grid did not return to one interval")
	}
}

func TestDispatchLagIsMeasuredBeforeTheCall(t *testing.T) {
	h := quietHarness()
	h.stopAfterSends(3)
	h.run(0)
	s := h.stats.Snapshot()
	if s.DispatchLag.Count != 3 || s.DispatchLag.Max > time.Millisecond {
		t.Errorf("dispatch lag = %+v, want three samples of no lag on virtual time", s.DispatchLag)
	}
}

func TestRegistrationAndLoginGoThroughTheLimiter(t *testing.T) {
	limiter := &countingLimiter{}
	h := newHarnessWithLimiter(limiter)
	h.api.record = func(call int, p sentPosition) gateway.Kind {
		if call == 2 {
			return gateway.Unauthorized
		}
		return gateway.OK
	}
	h.stopAfterSends(4)
	h.run(0)
	// register + login + the re-login; sending itself is never limited.
	if limiter.acquired != 3 || limiter.open != 0 {
		t.Errorf("acquired %d (want 3), still held %d (want 0)", limiter.acquired, limiter.open)
	}
}

func TestStopWhileWaitingForTheLimiterEndsTheCabber(t *testing.T) {
	h := newHarnessWithLimiter(refusingLimiter{})
	h.run(0)
	if h.api.registerCalls != 0 || h.cabber.State() != Stopped {
		t.Errorf("%d registrations, state %s", h.api.registerCalls, h.cabber.State())
	}
}

type countingLimiter struct{ acquired, open int }

func (l *countingLimiter) Acquire(context.Context) (func(), error) {
	l.acquired++
	l.open++
	return func() { l.open-- }, nil
}

type refusingLimiter struct{}

func (refusingLimiter) Acquire(context.Context) (func(), error) { return nil, context.Canceled }
