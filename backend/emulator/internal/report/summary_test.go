package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/emulator/internal/config"
	"github.com/Keane81/Cabby/backend/emulator/internal/fleet"
	"github.com/Keane81/Cabby/backend/emulator/internal/gateway"
	"github.com/Keane81/Cabby/backend/emulator/internal/stats"
)

func sampleInput() Input {
	p := config.Default()
	p.Cabbers, p.Interval, p.RampUp, p.Duration = 1000, 5*time.Second, 25*time.Second, 10*time.Minute
	p.RunID, p.Seed = "k3x9a2bd", 42
	start := time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)

	var c stats.Counters
	c.Started.Store(1000)
	c.OnSchedule.Store(980)
	c.SentOK.Store(118_000)
	c.Skipped.Store(12)
	c.LoggedOut.Store(1000)
	for i := 0; i < 3; i++ {
		c.SendFailed(gateway.Unavailable)
	}
	c.SendLatency.Record(8 * time.Millisecond)
	c.SendLatency.Record(21 * time.Millisecond)
	c.DispatchLag.Record(time.Millisecond)

	return Input{
		Profile: p,
		Result: fleet.Result{
			Cabbers: 1000, StartedAt: start, FinishedAt: start.Add(10*time.Minute + 2*time.Second),
			TimeToFull: 27 * time.Second, AttemptsAtFull: 2500,
			StoppedAt: start.Add(10 * time.Minute), StopTook: 1900 * time.Millisecond,
		},
		Snap: c.Snapshot(),
	}
}

func TestSummaryJSONHasTheDocumentedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	if err := Build(sampleInput()).WriteJSON(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	for _, key := range []string{"run_id", "seed", "profile", "started_at", "finished_at", "fleet", "sent", "rate", "latency_ms", "shutdown", "estimate", "verdict"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("summary lacks %q", key)
		}
	}
	fleetDoc := doc["fleet"].(map[string]any)
	for _, key := range []string{"started", "failed", "lost", "time_to_full_ms", "on_schedule_ratio"} {
		if _, ok := fleetDoc[key]; !ok {
			t.Errorf("fleet lacks %q", key)
		}
	}
	sent := doc["sent"].(map[string]any)["failed"].(map[string]any)
	for _, key := range []string{"unavailable", "timeout", "network", "unauthorized", "invalid_request"} {
		if _, ok := sent[key]; !ok {
			t.Errorf("sent.failed lacks %q: every kind is reported, zero included", key)
		}
	}
	if doc["verdict"] != "ok" || fleetDoc["time_to_full_ms"].(float64) != 27000 {
		t.Errorf("verdict %v, time to full %v", doc["verdict"], fleetDoc["time_to_full_ms"])
	}
}

func TestSummaryRateIsCountedOverTheSteadyState(t *testing.T) {
	r := Build(sampleInput())
	// (118000 + 3 - 2500) attempts over 600 - 27 seconds.
	want := float64(118_003-2_500) / 573
	if r.Rate.ActualPerSecond < want*0.999 || r.Rate.ActualPerSecond > want*1.001 {
		t.Errorf("actual rate %.2f, want %.2f", r.Rate.ActualPerSecond, want)
	}
	if r.Rate.TargetPerSecond != 200 {
		t.Errorf("target rate %.1f, want 200", r.Rate.TargetPerSecond)
	}
	if r.Fleet.OnScheduleRate != 0.98 {
		t.Errorf("on schedule ratio %.2f", r.Fleet.OnScheduleRate)
	}
}

func TestSummaryHasNoSecretsNorPositions(t *testing.T) {
	in := sampleInput()
	r := Build(in)
	encoded, _ := json.Marshal(r)
	text := string(encoded) + r.Text()
	for _, forbidden := range []string{"password", "token", "Bearer", "@emulator", "latitude", "longitude"} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(forbidden)) {
			t.Errorf("the summary mentions %q", forbidden)
		}
	}
}

func TestSummaryWithoutAFullParkSaysSo(t *testing.T) {
	in := sampleInput()
	in.Result.TimeToFull, in.Result.AttemptsAtFull = 0, 0
	text := Build(in).Text()
	if !strings.Contains(text, "did not reach full size") {
		t.Errorf("text %q", text)
	}
}

func TestSummaryTextNamesTheBottleneck(t *testing.T) {
	in := sampleInput()
	in.Snap.DispatchLag.P99 = 400 * time.Millisecond
	if text := Build(in).Text(); !strings.Contains(text, "could not produce the asked load") {
		t.Errorf("text %q", text)
	}
	in = sampleInput()
	in.Snap.SendLatency.P95 = 2 * time.Second
	if text := Build(in).Text(); !strings.Contains(text, "answered slowly or with errors") {
		t.Errorf("text %q", text)
	}
}

func TestWriteJSONReportsAnUnwritablePath(t *testing.T) {
	if err := Build(sampleInput()).WriteJSON(filepath.Join(t.TempDir(), "missing", "r.json")); err == nil {
		t.Fatal("no error for a missing directory")
	}
}
