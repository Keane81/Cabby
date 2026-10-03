package report

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/Keane81/Cabby/backend/emulator/internal/stats"
)

// LiveWindow is the span the live rate and error share are counted over.
const LiveWindow = 5 * time.Second

type sample struct {
	at       time.Time
	attempts int64
	failed   int64
}

// Source is what the live line reads.
type Source struct {
	Counters *stats.Counters
	Phase    func() string
	// Total is the number of cabbers of this process; Target is the sends per second they should make.
	Total  int64
	Target float64
	Start  time.Time
}

// Live builds the line printed every few seconds.
type Live struct {
	src     Source
	samples []sample
}

// NewLive starts a live view of a run.
func NewLive(src Source) *Live {
	// The window starts at the beginning of the run, so that the first line already has a rate.
	return &Live{src: src, samples: []sample{{at: src.Start}}}
}

// Line is the state of the run at now:
//
//	t=00:02:05 phase=steady active=1000/1000 sent=198.4/s target=200.0/s err=0.1% (unavailable=1) p50=8ms p95=21ms p99=40ms
func (l *Live) Line(now time.Time) string {
	snap := l.src.Counters.Snapshot()
	current := sample{at: now, attempts: snap.SentOK + snap.SendFailedTotal, failed: snap.SendFailedTotal}
	l.samples = append(l.samples, current)
	for len(l.samples) > 2 && now.Sub(l.samples[1].at) >= LiveWindow {
		l.samples = l.samples[1:]
	}
	oldest := l.samples[0]

	rate, errShare := 0.0, 0.0
	if span := current.at.Sub(oldest.at).Seconds(); span > 0 {
		rate = float64(current.attempts-oldest.attempts) / span
	}
	if delta := current.attempts - oldest.attempts; delta > 0 {
		errShare = float64(current.failed-oldest.failed) / float64(delta) * 100
	}

	var b strings.Builder
	fmt.Fprintf(&b, "t=%s phase=%s active=%d/%d sent=%.1f/s target=%.1f/s err=%.1f%%",
		clock(now.Sub(l.src.Start)), l.src.Phase(), snap.Active, l.src.Total, rate, l.src.Target, errShare)
	if kinds := failureKinds(snap.SendFailed); kinds != "" {
		fmt.Fprintf(&b, " (%s)", kinds)
	}
	fmt.Fprintf(&b, " p50=%s p95=%s p99=%s", ms(snap.SendLatency.P50), ms(snap.SendLatency.P95), ms(snap.SendLatency.P99))
	return b.String()
}

// Run prints a line every interval until ctx is done.
func (l *Live) Run(ctx context.Context, w io.Writer, every time.Duration, now func() time.Time) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			fmt.Fprintln(w, l.Line(now()))
		case <-ctx.Done():
			return
		}
	}
}

func failureKinds(failed map[string]int64) string {
	names := make([]string, 0, len(failed))
	for name := range failed {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s=%d", name, failed[name]))
	}
	return strings.Join(parts, " ")
}

func clock(d time.Duration) string {
	total := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", total/3600, total%3600/60, total%60)
}

func ms(d time.Duration) string { return fmt.Sprintf("%dms", d.Milliseconds()) }
