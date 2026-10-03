package report

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/emulator/internal/gateway"
	"github.com/Keane81/Cabby/backend/emulator/internal/stats"
)

func newLive(counters *stats.Counters, start time.Time, phase string) *Live {
	return NewLive(Source{Counters: counters, Phase: func() string { return phase }, Total: 1000, Target: 200, Start: start})
}

func TestLiveLineHasTheDocumentedFormat(t *testing.T) {
	counters := &stats.Counters{}
	start := time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)
	live := newLive(counters, start, "steady")
	counters.Active.Store(1000)
	counters.SentOK.Store(1000)
	counters.SendLatency.Record(8 * time.Millisecond)

	live.Line(start)
	counters.SentOK.Add(1000)
	line := live.Line(start.Add(5 * time.Second))

	format := regexp.MustCompile(`^t=00:00:05 phase=steady active=1000/1000 sent=200\.0/s target=200\.0/s err=0\.0% p50=\d+ms p95=\d+ms p99=\d+ms$`)
	if !format.MatchString(line) {
		t.Errorf("line %q does not match %s", line, format)
	}
}

func TestLiveRateIsOverASlidingWindow(t *testing.T) {
	counters := &stats.Counters{}
	start := time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)
	live := newLive(counters, start, "steady")

	live.Line(start)
	// A busy first ten seconds: 100 sends per second.
	for sec := 1; sec <= 10; sec++ {
		counters.SentOK.Add(100)
		live.Line(start.Add(time.Duration(sec) * time.Second))
	}
	// Then five quiet seconds: the window of 5 s must show the quiet, not the 10-second average.
	var line string
	for sec := 11; sec <= 15; sec++ {
		counters.SentOK.Add(10)
		line = live.Line(start.Add(time.Duration(sec) * time.Second))
	}
	if !strings.Contains(line, "sent=10.0/s") {
		t.Errorf("line %q: the rate is not over the last 5 s", line)
	}
}

func TestLiveListsOnlyNonZeroErrorKindsAndTheShare(t *testing.T) {
	counters := &stats.Counters{}
	start := time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)
	live := newLive(counters, start, "steady")
	live.Line(start)

	counters.SentOK.Add(90)
	for i := 0; i < 8; i++ {
		counters.SendFailed(gateway.Unavailable)
	}
	for i := 0; i < 2; i++ {
		counters.SendFailed(gateway.Timeout)
	}
	line := live.Line(start.Add(5 * time.Second))

	if !strings.Contains(line, "err=10.0% (timeout=2 unavailable=8)") {
		t.Errorf("line %q lacks the error share and the sorted non-zero kinds", line)
	}
	if strings.Contains(line, "network") {
		t.Errorf("line %q lists a kind that did not happen", line)
	}
}

func TestLiveShowsThePhase(t *testing.T) {
	counters := &stats.Counters{}
	start := time.Now()
	for _, phase := range []string{"ramp-up", "steady", "stopping"} {
		if line := newLive(counters, start, phase).Line(start.Add(time.Second)); !strings.Contains(line, "phase="+phase) {
			t.Errorf("line %q lacks phase %s", line, phase)
		}
	}
}

func TestLiveFirstLineHasNoRateYet(t *testing.T) {
	counters := &stats.Counters{}
	start := time.Now()
	line := newLive(counters, start, "ramp-up").Line(start)
	if !strings.Contains(line, "sent=0.0/s") || !strings.Contains(line, "err=0.0%") {
		t.Errorf("line %q", line)
	}
}
