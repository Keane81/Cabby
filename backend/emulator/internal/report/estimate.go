// Package report turns the counters of a run into what the user reads: the live line, the verdict
// of the self-check, the estimate of the database growth and the final summary.
package report

import (
	"fmt"
	"time"
)

const (
	// BytesPerRow is what one accepted location costs the location database with its index
	// (specs/004-cabber-location research.md R-02).
	BytesPerRow = 175
	// WarnGB is the growth that is announced before the run starts.
	WarnGB = 10.0
	gb     = 1e9
)

// Estimate is the growth of the database caused by the park.
type Estimate struct {
	RowsPerHour int64
	GBPerHour   float64
	// Rows and GB cover the requested Duration; both are zero for a run until a signal.
	Rows int64
	GB   float64
}

// EstimateGrowth computes the growth for cabbers sending every interval, for the given duration.
func EstimateGrowth(cabbers int, interval, duration time.Duration) Estimate {
	perSecond := float64(cabbers) / interval.Seconds()
	e := Estimate{
		RowsPerHour: int64(perSecond * 3600),
		GBPerHour:   perSecond * 3600 * BytesPerRow / gb,
	}
	if duration > 0 {
		e.Rows = int64(perSecond * duration.Seconds())
		e.GB = perSecond * duration.Seconds() * BytesPerRow / gb
	}
	return e
}

// Warn reports whether the growth is worth a warning before the start: the run's own total if it
// has a length, otherwise one hour of it.
func (e Estimate) Warn() bool {
	if e.Rows > 0 {
		return e.GB > WarnGB
	}
	return e.GBPerHour > WarnGB
}

// Message is the line printed at the start.
func (e Estimate) Message() string {
	if e.Rows > 0 {
		return fmt.Sprintf("the run will add about %d location rows (%.2f GB); one hour of this load is %.2f GB",
			e.Rows, e.GB, e.GBPerHour)
	}
	return fmt.Sprintf("the load adds about %d location rows (%.2f GB) per hour; data is not removed afterwards",
		e.RowsPerHour, e.GBPerHour)
}
