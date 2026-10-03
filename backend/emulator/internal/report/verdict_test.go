package report

import (
	"testing"
	"time"
)

func evidence() Evidence {
	return Evidence{
		TargetPerSecond: 1000, ActualPerSecond: 998, SteadySeconds: 60,
		DispatchLagP99: 5 * time.Millisecond, SendP95: 20 * time.Millisecond, ErrorRatio: 0.001,
	}
}

func TestVerdictOKWhenTheLoadWasProducedAndTheSystemKeptUp(t *testing.T) {
	if got := Judge(evidence()); got != OK {
		t.Errorf("verdict = %s", got)
	}
}

func TestVerdictGeneratorWhenItDispatchesLate(t *testing.T) {
	e := evidence()
	e.DispatchLagP99 = 300 * time.Millisecond
	if got := Judge(e); got != GeneratorSaturated {
		t.Errorf("verdict = %s", got)
	}
	// Even when the system is slow too: a late generator proves nothing about the system.
	e.SendP95 = 2 * time.Second
	if got := Judge(e); got != GeneratorSaturated {
		t.Errorf("late generator and slow system gave %s", got)
	}
}

func TestVerdictGeneratorWhenTheRateIsLowAndTheSystemIsFast(t *testing.T) {
	e := evidence()
	e.ActualPerSecond = 850 // 15% under
	if got := Judge(e); got != GeneratorSaturated {
		t.Errorf("verdict = %s", got)
	}
}

func TestVerdictToleratesTenPercentUnderTheTarget(t *testing.T) {
	e := evidence()
	e.ActualPerSecond = 905
	if got := Judge(e); got != OK {
		t.Errorf("verdict = %s", got)
	}
}

func TestVerdictSystemWhenLatencyOrErrorsAreHigh(t *testing.T) {
	slow := evidence()
	slow.SendP95 = 800 * time.Millisecond
	if got := Judge(slow); got != SystemSaturated {
		t.Errorf("slow system gave %s", got)
	}
	failing := evidence()
	failing.ErrorRatio = 0.2
	if got := Judge(failing); got != SystemSaturated {
		t.Errorf("failing system gave %s", got)
	}
	both := evidence()
	both.ActualPerSecond, both.SendP95 = 500, time.Second
	if got := Judge(both); got != SystemSaturated {
		t.Errorf("low rate with a slow system gave %s", got)
	}
}

func TestVerdictIgnoresTheRateOfAVeryShortSteadyState(t *testing.T) {
	e := evidence()
	e.SteadySeconds, e.ActualPerSecond = 0.4, 100
	if got := Judge(e); got != OK {
		t.Errorf("verdict = %s", got)
	}
}
