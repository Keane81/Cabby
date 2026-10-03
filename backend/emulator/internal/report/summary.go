package report

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Keane81/Cabby/backend/emulator/internal/config"
	"github.com/Keane81/Cabby/backend/emulator/internal/fleet"
	"github.com/Keane81/Cabby/backend/emulator/internal/stats"
)

// Input is everything a summary is made of.
type Input struct {
	Profile config.Profile
	Result  fleet.Result
	Snap    stats.Snapshot
}

// Report is the summary of a run (contracts/cli.md). Fields are only ever added, so that the
// summaries of different versions stay comparable. It never carries a password, token, email or
// coordinate.
type Report struct {
	RunID      string       `json:"run_id"`
	Seed       int64        `json:"seed"`
	Profile    ProfileInfo  `json:"profile"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt time.Time    `json:"finished_at"`
	Fleet      FleetInfo    `json:"fleet"`
	Sent       SentInfo     `json:"sent"`
	Rate       RateInfo     `json:"rate"`
	LatencyMS  LatencyInfo  `json:"latency_ms"`
	DispatchMS LatencyInfo  `json:"dispatch_lag_ms"`
	Shutdown   ShutdownInfo `json:"shutdown"`
	Estimate   EstimateInfo `json:"estimate"`
	Verdict    Verdict      `json:"verdict"`
}

type ProfileInfo struct {
	Cabbers    int    `json:"cabbers"`
	IntervalMS int64  `json:"interval_ms"`
	DurationMS int64  `json:"duration_ms"`
	RampUpMS   int64  `json:"ramp_up_ms"`
	Target     string `json:"target"`
	Instances  int    `json:"instances"`
	Instance   int    `json:"instance"`
}

type FleetInfo struct {
	Started        int64   `json:"started"`
	Failed         int64   `json:"failed"`
	Lost           int64   `json:"lost"`
	TimeToFullMS   int64   `json:"time_to_full_ms"`
	OnScheduleRate float64 `json:"on_schedule_ratio"`
}

type SentInfo struct {
	OK      int64            `json:"ok"`
	Failed  map[string]int64 `json:"failed"`
	Skipped int64            `json:"skipped"`
}

type RateInfo struct {
	TargetPerSecond float64 `json:"target_per_s"`
	ActualPerSecond float64 `json:"actual_per_s"`
}

type LatencyInfo struct {
	P50 int64 `json:"p50"`
	P95 int64 `json:"p95"`
	P99 int64 `json:"p99"`
	Max int64 `json:"max"`
}

type ShutdownInfo struct {
	LoggedOut    int64 `json:"logged_out"`
	NotLoggedOut int64 `json:"not_logged_out"`
	TookMS       int64 `json:"took_ms"`
}

// EstimateInfo counts the rows the accepted sends added; the size is an estimate (BytesPerRow).
type EstimateInfo struct {
	Rows int64   `json:"rows"`
	GB   float64 `json:"gb"`
}

var failureKindsReported = []string{"unavailable", "timeout", "network", "unauthorized", "invalid_request", "unexpected"}

// Build makes the summary of a finished run.
func Build(in Input) Report {
	snap, res, prof := in.Snap, in.Result, in.Profile

	failed := map[string]int64{}
	for _, kind := range failureKindsReported {
		failed[kind] = snap.SendFailed[kind]
	}

	attempts := snap.SentOK + snap.SendFailedTotal
	target := float64(res.Cabbers) / prof.Interval.Seconds()

	// The rate is counted over the steady state only: while the park is still growing it is lower
	// than the target by construction.
	steady, actual := 0.0, 0.0
	if res.TimeToFull > 0 {
		steady = res.StoppedAt.Sub(res.StartedAt.Add(res.TimeToFull)).Seconds()
		if steady > 0 {
			actual = float64(attempts-res.AttemptsAtFull) / steady
		}
	} else if total := res.StoppedAt.Sub(res.StartedAt).Seconds(); total > 0 {
		actual = float64(attempts) / total
	}

	errorRatio := 0.0
	if attempts > 0 {
		errorRatio = float64(snap.SendFailedTotal) / float64(attempts)
	}
	onSchedule := 0.0
	if res.Cabbers > 0 {
		onSchedule = float64(snap.OnSchedule) / float64(res.Cabbers)
	}

	return Report{
		RunID: prof.RunID, Seed: prof.Seed,
		Profile: ProfileInfo{
			Cabbers: prof.Cabbers, IntervalMS: prof.Interval.Milliseconds(), DurationMS: prof.Duration.Milliseconds(),
			RampUpMS: prof.RampUp.Milliseconds(), Target: prof.Target, Instances: prof.Instances, Instance: prof.Instance,
		},
		StartedAt: res.StartedAt.UTC(), FinishedAt: res.FinishedAt.UTC(),
		Fleet: FleetInfo{
			Started: snap.Started, Failed: snap.Failed, Lost: snap.Lost,
			TimeToFullMS: res.TimeToFull.Milliseconds(), OnScheduleRate: onSchedule,
		},
		Sent:      SentInfo{OK: snap.SentOK, Failed: failed, Skipped: snap.Skipped},
		Rate:      RateInfo{TargetPerSecond: target, ActualPerSecond: actual},
		LatencyMS: latency(snap.SendLatency), DispatchMS: latency(snap.DispatchLag),
		Shutdown: ShutdownInfo{LoggedOut: snap.LoggedOut, NotLoggedOut: snap.NotLoggedOut, TookMS: res.StopTook.Milliseconds()},
		Estimate: EstimateInfo{Rows: snap.SentOK, GB: float64(snap.SentOK) * BytesPerRow / gb},
		Verdict: Judge(Evidence{
			TargetPerSecond: target, ActualPerSecond: actual, SteadySeconds: steady,
			DispatchLagP99: snap.DispatchLag.P99, SendP95: snap.SendLatency.P95, ErrorRatio: errorRatio,
		}),
	}
}

func latency(s stats.Summary) LatencyInfo {
	return LatencyInfo{P50: s.P50.Milliseconds(), P95: s.P95.Milliseconds(), P99: s.P99.Milliseconds(), Max: s.Max.Milliseconds()}
}

// WriteJSON writes the summary to path.
func (r Report) WriteJSON(path string) error {
	encoded, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("report: encoding the summary: %w", err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("report: writing the summary: %w", err)
	}
	return nil
}

// Text is the summary for a person.
func (r Report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Run %s finished: verdict %s\n", r.RunID, r.Verdict)
	fmt.Fprintf(&b, "  park:     %d started, %d failed to start, %d lost", r.Fleet.Started, r.Fleet.Failed, r.Fleet.Lost)
	if r.Fleet.TimeToFullMS > 0 {
		fmt.Fprintf(&b, "; full after %s, %.0f%% began on schedule\n", time.Duration(r.Fleet.TimeToFullMS)*time.Millisecond, r.Fleet.OnScheduleRate*100)
	} else {
		fmt.Fprintf(&b, "; the park did not reach full size\n")
	}
	fmt.Fprintf(&b, "  sent:     %d accepted, %d skipped slots", r.Sent.OK, r.Sent.Skipped)
	if kinds := failureKinds(nonZero(r.Sent.Failed)); kinds != "" {
		fmt.Fprintf(&b, ", failed: %s", kinds)
	}
	fmt.Fprintf(&b, "\n  rate:     %.1f/s of %.1f/s asked\n", r.Rate.ActualPerSecond, r.Rate.TargetPerSecond)
	fmt.Fprintf(&b, "  latency:  p50=%dms p95=%dms p99=%dms max=%dms\n", r.LatencyMS.P50, r.LatencyMS.P95, r.LatencyMS.P99, r.LatencyMS.Max)
	fmt.Fprintf(&b, "  logout:   %d logged out, %d left to expire, %s\n", r.Shutdown.LoggedOut, r.Shutdown.NotLoggedOut,
		time.Duration(r.Shutdown.TookMS)*time.Millisecond)
	fmt.Fprintf(&b, "  database: about %d rows added (%.3f GB)\n", r.Estimate.Rows, r.Estimate.GB)
	switch r.Verdict {
	case GeneratorSaturated:
		b.WriteString("  note:     the emulator could not produce the asked load; run it in several processes (-instances) or lower the load\n")
	case SystemSaturated:
		b.WriteString("  note:     the emulator kept its schedule; the system answered slowly or with errors\n")
	}
	return b.String()
}

func nonZero(m map[string]int64) map[string]int64 {
	out := map[string]int64{}
	for k, v := range m {
		if v > 0 {
			out[k] = v
		}
	}
	return out
}
