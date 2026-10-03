package report

import "time"

// Verdict is the answer of the self-check: who was the bottleneck (research.md R-06, FR-014).
type Verdict string

const (
	// OK means the load was generated as asked and the system kept up.
	OK Verdict = "ok"
	// GeneratorSaturated means the emulator could not produce the asked load; the results below the
	// target say nothing about the system.
	GeneratorSaturated Verdict = "generator_saturated"
	// SystemSaturated means the emulator kept its schedule and the system answered slowly or with errors.
	SystemSaturated Verdict = "system_saturated"
)

const (
	maxDispatchLag   = 250 * time.Millisecond
	slowSendP95      = 500 * time.Millisecond
	maxErrorRatio    = 0.05
	rateTolerance    = 0.9
	minSteadySeconds = 1.0
)

// Evidence is what the verdict is made of.
type Evidence struct {
	TargetPerSecond float64
	ActualPerSecond float64
	// SteadySeconds is how long the full park was sending; below a second the rate says nothing.
	SteadySeconds  float64
	DispatchLagP99 time.Duration
	SendP95        time.Duration
	ErrorRatio     float64
}

// Judge applies the rules: a generator that dispatches late is saturated whatever the system does;
// a rate more than 10% under the target with a system that answers fast and cleanly is also the
// generator; a slow or failing system is the system.
func Judge(e Evidence) Verdict {
	systemSlow := e.SendP95 > slowSendP95 || e.ErrorRatio > maxErrorRatio
	rateLow := e.SteadySeconds >= minSteadySeconds && e.TargetPerSecond > 0 &&
		e.ActualPerSecond < e.TargetPerSecond*rateTolerance
	switch {
	case e.DispatchLagP99 > maxDispatchLag:
		return GeneratorSaturated
	case systemSlow:
		return SystemSaturated
	case rateLow:
		return GeneratorSaturated
	default:
		return OK
	}
}
