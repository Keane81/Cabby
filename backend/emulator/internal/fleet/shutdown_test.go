package fleet

import (
	"context"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/emulator/internal/stats"
)

func TestStopEndsSendingAtOnce(t *testing.T) {
	api := newFakeAPI()
	counters := &stats.Counters{}
	f := New(params(profile(20, 20*time.Millisecond, 0, 0), api, counters))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Result)
	go func() { done <- f.Run(ctx, context.Background()) }()

	time.Sleep(150 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the run did not return after the stop")
	}
	sent := api.records.Load()
	time.Sleep(100 * time.Millisecond)
	if after := api.records.Load(); after != sent {
		t.Errorf("%d positions were sent after the run returned", after-sent)
	}
	if s := counters.Snapshot(); s.Active != 0 {
		t.Errorf("%d cabbers still active", s.Active)
	}
}

func TestStopLogsEveryoneOutWithinTheDeadline(t *testing.T) {
	api := newFakeAPI()
	counters := &stats.Counters{}
	f := New(params(profile(50, 30*time.Millisecond, 0, 300*time.Millisecond), api, counters))
	result := f.Run(context.Background(), context.Background())

	s := counters.Snapshot()
	if s.LoggedOut != 50 || s.NotLoggedOut != 0 {
		t.Errorf("logged out %d, not logged out %d", s.LoggedOut, s.NotLoggedOut)
	}
	if result.StopTook > time.Second {
		t.Errorf("the logout phase took %s", result.StopTook)
	}
}

func TestSessionsThatMissTheDeadlineAreCounted(t *testing.T) {
	api := newFakeAPI()
	api.logoutDelay = 20 * time.Millisecond
	counters := &stats.Counters{}
	p := params(profile(100, 30*time.Millisecond, 0, 300*time.Millisecond), api, counters)
	p.LogoutConcurrency = 10
	p.ShutdownTimeout = 100 * time.Millisecond // room for about 10 workers × 5 logouts
	result := New(p).Run(context.Background(), context.Background())

	s := counters.Snapshot()
	if s.LoggedOut+s.NotLoggedOut != 100 {
		t.Fatalf("logged out %d + not logged out %d, want every session accounted for", s.LoggedOut, s.NotLoggedOut)
	}
	if s.LoggedOut == 0 || s.NotLoggedOut == 0 {
		t.Errorf("logged out %d, not logged out %d: the deadline should split the park", s.LoggedOut, s.NotLoggedOut)
	}
	if result.StopTook > 500*time.Millisecond {
		t.Errorf("the logout phase took %s, want about the 100 ms deadline", result.StopTook)
	}
}

func TestSecondSignalCutsTheLogoutShort(t *testing.T) {
	api := newFakeAPI()
	api.logoutDelay = time.Second
	counters := &stats.Counters{}
	p := params(profile(20, 30*time.Millisecond, 0, 200*time.Millisecond), api, counters)
	force, forceNow := context.WithCancel(context.Background())
	time.AfterFunc(250*time.Millisecond, forceNow) // the run ends at 200 ms, the logout is then pending
	started := time.Now()
	New(p).Run(context.Background(), force)

	if took := time.Since(started); took > 2*time.Second {
		t.Errorf("returned after %s, want right after the second signal", took)
	}
	if s := counters.Snapshot(); s.NotLoggedOut != 20 || s.LoggedOut != 0 {
		t.Errorf("logged out %d, not logged out %d", s.LoggedOut, s.NotLoggedOut)
	}
}

func TestACabberWithoutASessionIsNotCountedAsNotLoggedOut(t *testing.T) {
	api := newFakeAPI()
	counters := &stats.Counters{}
	// The run stops before the ramp-up reaches the last cabbers: they never registered.
	f := New(params(profile(10, 30*time.Millisecond, 10*time.Second, 150*time.Millisecond), api, counters))
	f.Run(context.Background(), context.Background())
	s := counters.Snapshot()
	if s.NotLoggedOut != 0 || s.LoggedOut != s.LoggedIn {
		t.Errorf("logged in %d, logged out %d, not logged out %d", s.LoggedIn, s.LoggedOut, s.NotLoggedOut)
	}
}
