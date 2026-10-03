package report

import (
	"strings"
	"testing"
	"time"
)

func TestEstimateRowsPerHourAndGB(t *testing.T) {
	cases := []struct {
		cabbers  int
		interval time.Duration
		rows     int64
		gbPerHr  float64
	}{
		{1000, 5 * time.Second, 720_000, 0.126},    // 200/s
		{10_000, 5 * time.Second, 7_200_000, 1.26}, // 2 000/s
		{50_000, 5 * time.Second, 36_000_000, 6.3}, // 10 000/s
		{1, time.Second, 3_600, 0.00063},
	}
	for _, tc := range cases {
		e := EstimateGrowth(tc.cabbers, tc.interval, 0)
		if e.RowsPerHour != tc.rows {
			t.Errorf("%d/%s: %d rows per hour, want %d", tc.cabbers, tc.interval, e.RowsPerHour, tc.rows)
		}
		if diff := e.GBPerHour - tc.gbPerHr; diff > tc.gbPerHr*0.01 || diff < -tc.gbPerHr*0.01 {
			t.Errorf("%d/%s: %.4f GB per hour, want %.4f", tc.cabbers, tc.interval, e.GBPerHour, tc.gbPerHr)
		}
	}
}

func TestEstimateForARunWithADuration(t *testing.T) {
	e := EstimateGrowth(10_000, 5*time.Second, 30*time.Minute)
	if e.Rows != 3_600_000 {
		t.Errorf("rows = %d, want 3.6 million", e.Rows)
	}
	if e.GB < 0.62 || e.GB > 0.64 {
		t.Errorf("GB = %.3f, want about 0.63", e.GB)
	}
}

func TestWarningAboveTenGigabytes(t *testing.T) {
	// 50 000 cabbers: 6.3 GB per hour, so 1 hour is quiet and 2 hours warn.
	if EstimateGrowth(50_000, 5*time.Second, time.Hour).Warn() {
		t.Error("6.3 GB warned")
	}
	if !EstimateGrowth(50_000, 5*time.Second, 2*time.Hour).Warn() {
		t.Error("12.6 GB did not warn")
	}
	// Until a signal: judged by one hour of the load.
	if EstimateGrowth(50_000, 5*time.Second, 0).Warn() {
		t.Error("6.3 GB per hour warned for a run without a length")
	}
	if !EstimateGrowth(50_000, 2*time.Second, 0).Warn() {
		t.Error("15.8 GB per hour did not warn for a run without a length")
	}
}

func TestEstimateMessageStatesTheNumbers(t *testing.T) {
	msg := EstimateGrowth(1000, 5*time.Second, time.Hour).Message()
	for _, want := range []string{"720000", "GB"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
}
