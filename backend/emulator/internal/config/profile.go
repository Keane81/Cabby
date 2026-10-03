// Package config holds the load profile of one emulator run: its defaults, its validation and the
// command-line flags that fill it.
package config

import (
	"fmt"
	"math"
	"time"
)

const (
	// MaxCabbers is the largest park one run may describe (spec Clarifications: 50 000).
	MaxCabbers = 50000
	// MinInterval keeps one cabber from sending faster than a real client ever would.
	MinInterval = 100 * time.Millisecond
	// minAreaSideKm is the shortest side of the movement area: a smaller one cannot hold a walk.
	minAreaSideKm = 1.0
	kmPerDegree   = 111.32
)

// ValidationError names the profile field that was rejected and why (FR-009).
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("config: %s: %s", e.Field, e.Reason)
}

// Area is the rectangle the cabbers move in, in degrees.
type Area struct {
	MinLat, MinLon, MaxLat, MaxLon float64
}

// DefaultArea is one conventional city of roughly 20 x 30 km.
var DefaultArea = Area{MinLat: 55.60, MinLon: 37.40, MaxLat: 55.90, MaxLon: 37.85}

// Profile is the full description of a run (data-model.md, Profile).
type Profile struct {
	Cabbers     int
	Interval    time.Duration
	Duration    time.Duration // 0 runs until a signal
	RampUp      time.Duration
	Area        Area
	Target      string
	AllowRemote bool
	Seed        int64
	RunID       string
	MaxConns    int
	Instances   int
	Instance    int
	ReportPath  string
}

// DefaultRampUp stretches the logins over max(10 s, n * 25 ms): password hashing in the auth
// service is the bottleneck of the onboarding (research.md R-03).
func DefaultRampUp(cabbers int) time.Duration {
	ramp := time.Duration(cabbers) * 25 * time.Millisecond
	if ramp < 10*time.Second {
		return 10 * time.Second
	}
	return ramp
}

// Default returns the profile of a run with no flags: a moderate park of one hundred cabbers.
func Default() Profile {
	const cabbers = 100
	return Profile{
		Cabbers:   cabbers,
		Interval:  5 * time.Second,
		RampUp:    DefaultRampUp(cabbers),
		Area:      DefaultArea,
		Target:    "http://127.0.0.1:8080",
		MaxConns:  512,
		Instances: 1,
	}
}

// Validate rejects the profile as a whole: the first offending field is reported and nothing has
// been sent to the target by then (FR-009).
func (p Profile) Validate() error {
	switch {
	case p.Cabbers < 1 || p.Cabbers > MaxCabbers:
		return &ValidationError{"cabbers", fmt.Sprintf("must be between 1 and %d", MaxCabbers)}
	case p.Interval < MinInterval:
		return &ValidationError{"interval", fmt.Sprintf("must be at least %s", MinInterval)}
	case p.Duration < 0:
		return &ValidationError{"duration", "must not be negative"}
	case p.RampUp < 0:
		return &ValidationError{"ramp-up", "must not be negative"}
	case p.MaxConns < 1:
		return &ValidationError{"max-conns", "must be at least 1"}
	case p.Instances < 1:
		return &ValidationError{"instances", "must be at least 1"}
	case p.Instance < 0 || p.Instance >= p.Instances:
		return &ValidationError{"instance", "must be at least 0 and below instances"}
	}
	if err := p.Area.validate(); err != nil {
		return err
	}
	return validateTarget(p.Target, p.AllowRemote)
}

func (a Area) validate() error {
	for _, lat := range []float64{a.MinLat, a.MaxLat} {
		if math.IsNaN(lat) || lat < -90 || lat > 90 {
			return &ValidationError{"area", "latitude must be between -90 and 90"}
		}
	}
	for _, lon := range []float64{a.MinLon, a.MaxLon} {
		if math.IsNaN(lon) || lon < -180 || lon > 180 {
			return &ValidationError{"area", "longitude must be between -180 and 180"}
		}
	}
	if a.MinLat >= a.MaxLat || a.MinLon >= a.MaxLon {
		return &ValidationError{"area", "must have a positive size"}
	}
	heightKm := (a.MaxLat - a.MinLat) * kmPerDegree
	midLat := (a.MinLat + a.MaxLat) / 2
	widthKm := (a.MaxLon - a.MinLon) * kmPerDegree * math.Cos(midLat*math.Pi/180)
	if heightKm < minAreaSideKm || widthKm < minAreaSideKm {
		return &ValidationError{"area", fmt.Sprintf("each side must be at least %.0f km", minAreaSideKm)}
	}
	return nil
}
