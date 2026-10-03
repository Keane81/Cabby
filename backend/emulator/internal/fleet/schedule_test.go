package fleet

import (
	"context"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/emulator/internal/stats"
)

func TestSteadyRateMatchesCabbersOverInterval(t *testing.T) {
	api := newFakeAPI()
	counters := &stats.Counters{}
	const cabbers = 40
	interval := 50 * time.Millisecond
	f := New(params(profile(cabbers, interval, 0, 1000*time.Millisecond), api, counters))
	f.Run(context.Background(), context.Background())

	// Every cabber has a send phase inside its first interval, so it sends about
	// (duration - login) / interval times; 40 cabbers over ~0.95 s at 20 sends/s each.
	got := float64(counters.Snapshot().SentOK)
	want := float64(cabbers) * float64(900*time.Millisecond) / float64(interval)
	if got < want*0.7 || got > want*1.3 {
		t.Errorf("%v sends, want about %v", got, want)
	}
	if s := counters.Snapshot(); s.Skipped != 0 {
		t.Errorf("%d skipped slots on a gateway that answers at once", s.Skipped)
	}
}

func TestSlowGatewayShowsAsSkippedSlotsNotAsABurst(t *testing.T) {
	api := newFakeAPI()
	api.locationDelay = 120 * time.Millisecond // longer than the 40 ms interval
	counters := &stats.Counters{}
	f := New(params(profile(5, 40*time.Millisecond, 0, 800*time.Millisecond), api, counters))
	f.Run(context.Background(), context.Background())

	s := counters.Snapshot()
	if s.Skipped == 0 {
		t.Error("slots that fell inside slow sends were not counted")
	}
	// One request at a time per cabber: it can never have sent more than one per 120 ms.
	maxSends := int64(5) * int64(800*time.Millisecond/(120*time.Millisecond)+2)
	if api.records.Load() > maxSends {
		t.Errorf("%d sends, a burst: no more than %d are possible at one at a time", api.records.Load(), maxSends)
	}
}
