package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/emulator/internal/report"
)

// stubGateway answers the four operations of the contract and the health check, and counts every
// request it sees.
type stubGateway struct {
	server    *httptest.Server
	requests  atomic.Int64
	registers atomic.Int64
	locations atomic.Int64
	logouts   atomic.Int64
	// registerStatus overrides the answer to registration when not zero.
	registerStatus int
}

func newStub(t *testing.T) *stubGateway {
	t.Helper()
	s := &stubGateway{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/cabbers":
			s.registers.Add(1)
			if s.registerStatus != 0 {
				w.WriteHeader(s.registerStatus)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"cabber_id":"1","email":"x"}`)
		case r.URL.Path == "/cabber/session" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"access_token":"tok","expires_at":"2030-01-01T00:00:00Z"}`)
		case r.URL.Path == "/cabber/session" && r.Method == http.MethodDelete:
			s.logouts.Add(1)
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/cabber/location":
			s.locations.Add(1)
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

func runWith(args []string, signals <-chan os.Signal) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut, signals)
	return code, out.String(), errOut.String()
}

func TestInvalidProfileExitsWithTwoAndSendsNothing(t *testing.T) {
	stub := newStub(t)
	for _, args := range [][]string{
		{"-target", stub.server.URL, "-cabbers", "0"},
		{"-target", stub.server.URL, "-cabbers", "-4"},
		{"-target", stub.server.URL, "-interval", "0s"},
		{"-target", stub.server.URL, "-area", "1,2,3"},
		{"-target", stub.server.URL, "-instances", "2", "-instance", "2"},
	} {
		if code, _, stderr := runWith(args, nil); code != 2 {
			t.Errorf("%v: exit code %d, want 2\n%s", args, code, stderr)
		}
	}
	if n := stub.requests.Load(); n != 0 {
		t.Errorf("the target saw %d requests from invalid profiles, want none", n)
	}
}

func TestRemoteTargetWithoutTheFlagExitsWithTwo(t *testing.T) {
	code, _, stderr := runWith([]string{"-target", "https://example.com", "-cabbers", "1"}, nil)
	if code != 2 || !strings.Contains(stderr, "allow-remote") {
		t.Errorf("exit code %d, stderr %q", code, stderr)
	}
}

func TestUnreachableTargetExitsWithOne(t *testing.T) {
	stub := newStub(t)
	url := stub.server.URL
	stub.server.Close()
	code, _, stderr := runWith([]string{"-target", url, "-cabbers", "1"}, nil)
	if code != 1 || !strings.Contains(stderr, "emulator stopped with error") {
		t.Errorf("exit code %d, stderr %q", code, stderr)
	}
}

func TestHelpExitsWithZero(t *testing.T) {
	code, _, stderr := runWith([]string{"-h"}, nil)
	if code != 0 || !strings.Contains(stderr, "-cabbers") {
		t.Errorf("exit code %d, stderr %q", code, stderr)
	}
}

func TestNoCabberStartedExitsWithOne(t *testing.T) {
	stub := newStub(t)
	stub.registerStatus = http.StatusBadRequest // a permanent failure: not retried
	reportPath := filepath.Join(t.TempDir(), "r.json")
	code, _, _ := runWith([]string{"-target", stub.server.URL, "-cabbers", "2", "-interval", "100ms",
		"-ramp-up", "10ms", "-duration", "300ms", "-report", reportPath}, nil)
	if code != 1 {
		t.Errorf("exit code %d, want 1", code)
	}
}

func TestSuccessfulRunExitsWithZeroAndWritesTheSummary(t *testing.T) {
	stub := newStub(t)
	reportPath := filepath.Join(t.TempDir(), "r.json")
	code, stdout, stderr := runWith([]string{"-target", stub.server.URL, "-cabbers", "3", "-interval", "100ms",
		"-ramp-up", "100ms", "-duration", "500ms", "-report", reportPath}, nil)
	if code != 0 {
		t.Fatalf("exit code %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "verdict ok") {
		t.Errorf("stdout lacks the summary:\n%s", stdout)
	}
	for _, want := range []string{"emulator starting", "estimated database growth", "emulator stopped"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	raw, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Verdict string `json:"verdict"`
		Fleet   struct {
			Started int `json:"started"`
		} `json:"fleet"`
		Shutdown struct {
			LoggedOut int `json:"logged_out"`
		} `json:"shutdown"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Fleet.Started != 3 || doc.Shutdown.LoggedOut != 3 || doc.Verdict != "ok" {
		t.Errorf("summary = %+v", doc)
	}
	if stub.registers.Load() != 3 || stub.locations.Load() == 0 || stub.logouts.Load() != 3 {
		t.Errorf("the gateway saw %d registrations, %d locations, %d logouts", stub.registers.Load(), stub.locations.Load(), stub.logouts.Load())
	}
}

func TestLogLinesCarryNoSecrets(t *testing.T) {
	stub := newStub(t)
	_, _, stderr := runWith([]string{"-target", stub.server.URL, "-cabbers", "2", "-interval", "100ms",
		"-ramp-up", "10ms", "-duration", "300ms", "-report", filepath.Join(t.TempDir(), "r.json")}, nil)
	for _, forbidden := range []string{"password", "Bearer", "access_token", "emulator.cabby.test", "latitude"} {
		if strings.Contains(stderr, forbidden) {
			t.Errorf("stderr mentions %q:\n%s", forbidden, stderr)
		}
	}
}

func TestFirstSignalStopsTheRun(t *testing.T) {
	stub := newStub(t)
	signals := make(chan os.Signal, 2)
	time.AfterFunc(300*time.Millisecond, func() { signals <- os.Interrupt })
	started := time.Now()
	code, stdout, _ := runWith([]string{"-target", stub.server.URL, "-cabbers", "3", "-interval", "100ms",
		"-ramp-up", "50ms", "-report", filepath.Join(t.TempDir(), "r.json")}, signals) // no duration: until a signal
	if code != 0 || time.Since(started) > 5*time.Second {
		t.Errorf("exit code %d after %s", code, time.Since(started))
	}
	if !strings.Contains(stdout, "3 logged out") || stub.logouts.Load() != 3 {
		t.Errorf("the park was not logged out:\n%s", stdout)
	}
}

func TestExitCodeMapping(t *testing.T) {
	started := report.Report{}
	started.Fleet.Started = 5

	saturated := started
	saturated.Verdict = report.GeneratorSaturated
	system := started
	system.Verdict = report.SystemSaturated

	cases := []struct {
		name    string
		rep     report.Report
		cabbers int
		want    int
	}{
		{"a normal run", started, 5, exitOK},
		{"a saturated generator", saturated, 5, exitSaturated},
		{"a saturated system is a result, not a failure of the tool", system, 5, exitOK},
		{"nobody started", report.Report{}, 5, exitFailed},
		{"nothing to start", report.Report{}, 0, exitOK},
	}
	for _, tc := range cases {
		if got := exitCodeFor(tc.rep, tc.cabbers); got != tc.want {
			t.Errorf("%s: exit code %d, want %d", tc.name, got, tc.want)
		}
	}
}
