package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/Keane81/Cabby/backend/emulator/internal/config"
	"github.com/Keane81/Cabby/backend/emulator/internal/fleet"
	"github.com/Keane81/Cabby/backend/emulator/internal/gateway"
	"github.com/Keane81/Cabby/backend/emulator/internal/report"
	"github.com/Keane81/Cabby/backend/emulator/internal/stats"
)

// Exit codes (contracts/cli.md).
const (
	exitOK        = 0
	exitFailed    = 1 // the target is unreachable at the start, or no cabber could start
	exitInvalid   = 2 // an invalid profile: nothing was sent
	exitSaturated = 3 // the run is over, but the emulator could not produce the asked load
)

// liveEvery is the period of the live line.
const liveEvery = 5 * time.Second

// descriptorReserve is what the process needs besides the pooled connections.
const descriptorReserve = 64

// run is main without the process: it takes the streams and the signals, so that tests can drive
// it. The logger writes to stderr, not to stdout as in the services: stdout carries the summary of
// the run and must stay free of log lines so that it can be piped.
func run(args []string, stdout, stderr io.Writer, signals <-chan os.Signal) int {
	stderr = &lockedWriter{w: stderr}
	log := zerolog.New(stderr).With().Timestamp().Logger()

	profile, err := config.Parse(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	if err != nil {
		log.Error().Str("error_class", "invalid_profile").Err(err).Msg("emulator stopped with error")
		return exitInvalid
	}
	if err := config.RaiseFileLimit(uint64(profile.MaxConns) + descriptorReserve); err != nil {
		log.Error().Str("error_class", "file_limit").Err(err).Msg("emulator stopped with error")
		return exitInvalid
	}

	client := gateway.New(profile.Target, profile.MaxConns)
	if kind := client.Health(context.Background()); kind != gateway.OK {
		log.Error().Str("error_class", kind.String()).Str("operation", "health").Msg("emulator stopped with error")
		return exitFailed
	}

	log.Info().
		Str("run_id", profile.RunID).Int64("seed", profile.Seed).Str("target", profile.Target).
		Int("cabbers", profile.Cabbers).Int64("interval_ms", profile.Interval.Milliseconds()).
		Int64("duration_ms", profile.Duration.Milliseconds()).Int64("ramp_up_ms", profile.RampUp.Milliseconds()).
		Int("instances", profile.Instances).Int("instance", profile.Instance).Int("max_conns", profile.MaxConns).
		Msg("emulator starting")
	growth := report.EstimateGrowth(profile.Cabbers, profile.Interval, profile.Duration)
	log.Info().Int64("rows_per_hour", growth.RowsPerHour).Float64("gb_per_hour", growth.GBPerHour).
		Msg("estimated database growth")
	if growth.Warn() {
		log.Warn().Int64("rows", growth.Rows).Float64("gb", growth.GB).Float64("gb_per_hour", growth.GBPerHour).
			Msg("database growth above 10 GB")
	}

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	force, forceStop := context.WithCancel(context.Background())
	defer forceStop()
	go func() {
		// The first signal stops the run, the second cuts the logout short.
		select {
		case <-signals:
			stop()
		case <-ctx.Done():
			return
		}
		select {
		case <-signals:
			forceStop()
		case <-force.Done():
		}
	}()

	counters := &stats.Counters{}
	park := fleet.New(fleet.Params{Profile: profile, API: client, Stats: counters, Log: log})
	owned := len(fleet.Indices(profile.Cabbers, profile.Instances, profile.Instance))
	start := time.Now()
	live := report.NewLive(report.Source{
		Counters: counters, Phase: func() string { return park.Phase().String() },
		Total: int64(owned), Target: float64(owned) / profile.Interval.Seconds(), Start: start,
	})
	liveCtx, liveStop := context.WithCancel(context.Background())
	go live.Run(liveCtx, stderr, liveEvery, time.Now)

	result := park.Run(ctx, force)
	liveStop()

	rep := report.Build(report.Input{Profile: profile, Result: result, Snap: counters.Snapshot()})
	fmt.Fprint(stdout, rep.Text())

	code := exitCodeFor(rep, result.Cabbers)
	if err := rep.WriteJSON(profile.ReportPath); err != nil {
		log.Error().Str("error_class", "report").Err(err).Msg("emulator stopped with error")
		return exitFailed
	}
	if code == exitFailed {
		log.Error().Str("error_class", "no_cabber_started").Msg("emulator stopped with error")
		return code
	}
	log.Info().Str("verdict", string(rep.Verdict)).Str("report", profile.ReportPath).Msg("emulator stopped")
	return code
}

// exitCodeFor maps the result of a run to the process exit code.
func exitCodeFor(rep report.Report, cabbers int) int {
	switch {
	case cabbers > 0 && rep.Fleet.Started == 0:
		return exitFailed
	case rep.Verdict == report.GeneratorSaturated:
		return exitSaturated
	default:
		return exitOK
	}
}

// lockedWriter makes stderr safe for the cabbers' log lines and the live line, which are written
// from different goroutines: every line reaches the stream whole, never interleaved.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
