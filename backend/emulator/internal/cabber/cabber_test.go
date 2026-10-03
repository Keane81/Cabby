package cabber

import (
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/emulator/internal/gateway"
)

func TestHappyPathLifecycle(t *testing.T) {
	h := quietHarness()
	h.stopAfterSends(5)
	h.run(0)

	if got := h.api.order[:2]; got[0] != "register" || got[1] != "login" {
		t.Fatalf("calls start with %v, want register then login", got)
	}
	s := h.stats.Snapshot()
	if s.Registered != 1 || s.LoggedIn != 1 || s.Started != 1 || s.SentOK != 5 || s.Active != 0 {
		t.Fatalf("stats = %+v", s)
	}
	if h.cabber.State() != Stopped {
		t.Errorf("state = %s, want stopped", h.cabber.State())
	}
	had, kind := h.cabber.Logout(h.ctxBackground())
	if !had || kind != gateway.OK || h.cabber.State() != Done || h.stats.Snapshot().LoggedOut != 1 {
		t.Fatalf("logout = %v %s, state %s", had, kind, h.cabber.State())
	}
}

func TestCoordinatesAreSentOnlyAfterLoginAndWithTheSessionToken(t *testing.T) {
	h := quietHarness()
	h.stopAfterSends(3)
	h.run(0)

	for i, kind := range h.api.order {
		if kind == "record" && i < 2 {
			t.Fatalf("a position was sent before the login: %v", h.api.order)
		}
	}
	for _, p := range h.api.sent {
		if p.token != "token-1" {
			t.Errorf("sent with token %q, want the one the login returned", p.token)
		}
	}
}

func TestSendsAreOneIntervalApartStartingAtThePhase(t *testing.T) {
	h := quietHarness()
	h.stopAfterSends(6)
	loggedInAt := h.clock.Now()
	h.run(2 * time.Second)

	if first := h.api.sent[0].at.Sub(loggedInAt); first != 2*time.Second {
		t.Errorf("first send %s after the login, want the 2s phase", first)
	}
	for i := 1; i < len(h.api.sent); i++ {
		if d := h.api.sent[i].at.Sub(h.api.sent[i-1].at); d != testInterval {
			t.Errorf("sends %d and %d are %s apart, want %s", i-1, i, d, testInterval)
		}
	}
}

func TestPositionsMoveWithoutTeleporting(t *testing.T) {
	h := quietHarness()
	h.stopAfterSends(200)
	h.run(0)

	limit := h.cabber.walker.SpeedMS() * testInterval.Seconds() * 1.01
	for i := 1; i < len(h.api.sent); i++ {
		a, b := h.api.sent[i-1], h.api.sent[i]
		dLat := (b.lat - a.lat) * 111_320
		dLon := (b.lon - a.lon) * 111_320 * 0.56 // cos(55.75°)
		if dist := dLat*dLat + dLon*dLon; dist > limit*limit {
			t.Fatalf("send %d jumped further than %.1f m", i, limit)
		}
	}
}

func TestCancellationStopsSendingAtOnce(t *testing.T) {
	h := quietHarness()
	h.stopAfterSends(3)
	h.run(0)
	if h.api.recordCalls != 3 {
		t.Fatalf("%d positions sent, want exactly 3 and none after the cancellation", h.api.recordCalls)
	}
	if h.stats.Snapshot().Active != 0 {
		t.Error("the cabber is still counted as active")
	}
}

func TestStopBeforeStartSendsNothing(t *testing.T) {
	h := quietHarness()
	h.cancel()
	h.run(0)
	if h.api.registerCalls != 0 || h.api.recordCalls != 0 {
		t.Fatalf("calls after a stop before the start: %v", h.api.order)
	}
	if h.cabber.State() != Stopped {
		t.Errorf("state = %s", h.cabber.State())
	}
}

func TestWaitsForItsMomentOfTheRamp(t *testing.T) {
	h := quietHarness()
	startAt := h.clock.Now().Add(40 * time.Second)
	h.stopAfterSends(1)
	h.cabber.Run(h.ctx, startAt, 0)
	if h.clock.sleeps[0] != 40*time.Second {
		t.Errorf("first sleep %s, want 40s", h.clock.sleeps[0])
	}
	if s := h.stats.Snapshot(); s.OnSchedule != 1 {
		t.Errorf("on schedule = %d, want 1", s.OnSchedule)
	}
}

func TestLateStartIsNotOnSchedule(t *testing.T) {
	h := quietHarness()
	h.stopAfterSends(1)
	h.cabber.Run(h.ctx, h.clock.Now().Add(-10*time.Second), 0)
	if s := h.stats.Snapshot(); s.OnSchedule != 0 {
		t.Errorf("on schedule = %d, want 0 for a start 10 s late", s.OnSchedule)
	}
}

func TestIdentityIsDeterministicAndWithinTheContract(t *testing.T) {
	a, b := NewIdentity("abc12345", 9, 4), NewIdentity("abc12345", 9, 4)
	if a != b {
		t.Fatal("same inputs gave different identities")
	}
	if a.Email(0) != "emu-abc12345-4@emulator.cabby.test" || a.Name != "emu-4" {
		t.Errorf("email %q, name %q", a.Email(0), a.Name)
	}
	if a.Email(1) != "emu-abc12345-4r1@emulator.cabby.test" {
		t.Errorf("retry email %q", a.Email(1))
	}
	if len(a.Password) < 4 || len(a.Password) > 16 {
		t.Errorf("password length %d outside 4–16", len(a.Password))
	}
	for _, other := range []Identity{NewIdentity("abc12345", 9, 5), NewIdentity("abc12346", 9, 4), NewIdentity("abc12345", 10, 4)} {
		if other.Password == a.Password {
			t.Error("a different index, run or seed gave the same password")
		}
	}
}

func TestLogoutWithoutSessionIsANoOp(t *testing.T) {
	h := quietHarness()
	if had, _ := h.cabber.Logout(h.ctxBackground()); had || h.api.logoutCalls != 0 {
		t.Fatal("logged out a cabber that never had a session")
	}
}

func TestLogoutOfARevokedSessionLeavesNothingToRevoke(t *testing.T) {
	h := quietHarness()
	h.stopAfterSends(1)
	h.run(0)
	h.api.logout = func(int, string) gateway.Kind { return gateway.Unauthorized }
	h.cabber.Logout(h.ctxBackground())
	s := h.stats.Snapshot()
	if h.cabber.State() != Done || s.NotLoggedOut != 0 || s.LogoutFailed != 0 {
		t.Errorf("state %s, stats %+v", h.cabber.State(), s)
	}
}
