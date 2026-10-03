package fleet

import (
	"context"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/emulator/internal/cabber"
	"github.com/Keane81/Cabby/backend/emulator/internal/stats"
)

func TestInstancesOwnDisjointIndicesThatCoverThePark(t *testing.T) {
	const cabbers = 1003
	for _, instances := range []int{1, 2, 4, 7} {
		seen := make([]int, cabbers)
		for instance := 0; instance < instances; instance++ {
			for _, index := range Indices(cabbers, instances, instance) {
				if index%instances != instance {
					t.Fatalf("instance %d/%d owns index %d", instance, instances, index)
				}
				seen[index]++
			}
		}
		for index, n := range seen {
			if n != 1 {
				t.Fatalf("%d instances: index %d is owned %d times", instances, index, n)
			}
		}
	}
}

func TestInstanceOwnsNothingBeyondThePark(t *testing.T) {
	if got := Indices(3, 5, 4); len(got) != 0 {
		t.Errorf("instance 4 of 5 over 3 cabbers owns %v", got)
	}
}

func TestEmailsAndPasswordsAgreeAcrossInstances(t *testing.T) {
	// The identity depends on run id, seed and index only, never on the instance.
	a := cabber.NewIdentity("run", 5, 41)
	b := cabber.NewIdentity("run", 5, 41)
	if a != b || a.Email(0) != b.Email(0) {
		t.Error("two processes derive different identities for the same cabber")
	}
}

func TestStartIsLinearOverTheRampUpAndGlobalByIndex(t *testing.T) {
	start := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	const cabbers = 1000
	ramp := 25 * time.Second
	if got := StartAt(start, 0, cabbers, ramp); !got.Equal(start) {
		t.Errorf("cabber 0 starts at %s", got.Sub(start))
	}
	if got := StartAt(start, 500, cabbers, ramp); got.Sub(start) != 12500*time.Millisecond {
		t.Errorf("cabber 500 starts at %s, want 12.5s", got.Sub(start))
	}
	if got := StartAt(start, cabbers-1, cabbers, ramp); got.Sub(start) >= ramp {
		t.Errorf("the last cabber starts at %s, not inside the ramp-up", got.Sub(start))
	}
	for i := 1; i < cabbers; i++ {
		if !StartAt(start, i, cabbers, ramp).After(StartAt(start, i-1, cabbers, ramp)) {
			t.Fatalf("starts are not increasing at %d", i)
		}
	}
	if !StartAt(start, 7, cabbers, 0).Equal(start) {
		t.Error("a zero ramp-up must start everyone at once")
	}
}

func TestSendPhasesAreSpreadEvenlyOverTheInterval(t *testing.T) {
	const cabbers, buckets = 10_000, 10
	interval := 5 * time.Second
	var counts [buckets]int
	for i := 0; i < cabbers; i++ {
		phase := SendPhase(3, i, interval)
		if phase < 0 || phase >= interval {
			t.Fatalf("phase %s outside [0, %s)", phase, interval)
		}
		counts[int(int64(phase)*buckets/int64(interval))]++
	}
	for b, n := range counts {
		if n < cabbers/buckets*8/10 || n > cabbers/buckets*12/10 {
			t.Errorf("bucket %d holds %d of %d phases, want about %d", b, n, cabbers, cabbers/buckets)
		}
	}
}

func TestSendPhaseIsDeterministic(t *testing.T) {
	if SendPhase(3, 77, time.Second) != SendPhase(3, 77, time.Second) {
		t.Error("the same seed and index gave different phases")
	}
	if SendPhase(3, 77, time.Second) == SendPhase(3, 78, time.Second) {
		t.Error("neighbouring cabbers share a phase")
	}
}

func TestTwoProcessesOfOneRunRegisterEachCabberOnce(t *testing.T) {
	api := newFakeAPI()
	const cabbers = 24
	for instance := 0; instance < 2; instance++ {
		p := profile(cabbers, 30*time.Millisecond, 0, 200*time.Millisecond)
		p.Instances, p.Instance = 2, instance
		result := New(params(p, api, &stats.Counters{})).Run(context.Background(), context.Background())
		if result.Cabbers != cabbers/2 {
			t.Fatalf("instance %d owns %d cabbers, want %d", instance, result.Cabbers, cabbers/2)
		}
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.order) != cabbers || len(api.registers) != cabbers {
		t.Errorf("%d registrations for %d distinct addresses, want %d of each", len(api.order), len(api.registers), cabbers)
	}
}
