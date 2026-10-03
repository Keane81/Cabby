//go:build load

package fleet

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/emulator/internal/gateway"
	"github.com/Keane81/Cabby/backend/emulator/internal/stats"
)

// TestFivethousandCabbersReachTheTargetRate is a load test and not part of `make check`; it is
// started by hand: go test -tags load -run Fivethousand ./internal/fleet
func TestFivethousandCabbersReachTheTargetRate(t *testing.T) {
	const cabbers = 5000
	interval := time.Second
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/cabber/session" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"access_token":"t","expires_at":"2030-01-01T00:00:00Z"}`))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusCreated)
		}
	}))
	defer server.Close()

	baseline := runtime.NumGoroutine()
	counters := &stats.Counters{}
	p := profile(cabbers, interval, 3*time.Second, 15*time.Second)
	p.MaxConns = 256
	run := params(p, nil, counters)
	run.API = gateway.New(server.URL, p.MaxConns)
	run.AuthConcurrency = 64 // the stub hashes nothing
	result := New(run).Run(context.Background(), context.Background())

	steady := (15*time.Second - result.TimeToFull).Seconds()
	got := float64(counters.Snapshot().SentOK) / steady
	want := float64(cabbers) / interval.Seconds()
	if got < want*0.9 || got > want*1.1 {
		t.Errorf("%.0f sends/s over %.1f s of steady state, want %.0f ±10%%", got, steady, want)
	}

	time.Sleep(500 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > baseline+50 {
		t.Errorf("%d goroutines after the run, %d before: a leak", after, baseline)
	}
}
