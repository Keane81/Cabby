// Package route moves one emulated cabber: a random walk with memory of its heading, a speed
// of a city car and a border it never crosses (research.md R-05).
package route

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/Keane81/Cabby/backend/emulator/internal/config"
)

const (
	metersPerDegree = 111_320.0
	minSpeedMS      = 20.0 / 3.6
	maxSpeedMS      = 50.0 / 3.6
	// turnSigma is the standard deviation of the heading change per step, in radians (20°).
	turnSigma = 20.0 * math.Pi / 180
	// coordinateScale rounds to seven decimals, the precision the contract keeps.
	coordinateScale = 1e7
)

// Walker is the movement of one cabber. It is not safe for concurrent use: one goroutine owns it.
type Walker struct {
	area     config.Area
	rng      *rand.Rand
	lat, lon float64
	speedMS  float64
	heading  float64 // radians clockwise from north
}

// New places the cabber with the given index. The same area, seed and index always produce the
// same sequence, and two indices produce different ones.
func New(area config.Area, seed int64, index int) *Walker {
	rng := rand.New(rand.NewPCG(uint64(seed), uint64(index)+1))
	return &Walker{
		area:    area,
		rng:     rng,
		lat:     area.MinLat + rng.Float64()*(area.MaxLat-area.MinLat),
		lon:     area.MinLon + rng.Float64()*(area.MaxLon-area.MinLon),
		speedMS: minSpeedMS + rng.Float64()*(maxSpeedMS-minSpeedMS),
		heading: rng.Float64() * 2 * math.Pi,
	}
}

// SpeedMS is the constant speed of this cabber in metres per second.
func (w *Walker) SpeedMS() float64 { return w.speedMS }

// Position is the current position, rounded to seven decimals.
func (w *Walker) Position() (lat, lon float64) {
	return round(w.lat), round(w.lon)
}

// Step moves the cabber for dt and returns the new position, rounded to seven decimals. The
// distance covered is speed × dt, so a longer dt after skipped sends is still a believable move.
func (w *Walker) Step(dt time.Duration) (lat, lon float64) {
	w.heading += w.rng.NormFloat64() * turnSigma
	distance := w.speedMS * dt.Seconds()
	north := distance * math.Cos(w.heading)
	east := distance * math.Sin(w.heading)

	w.lat += north / metersPerDegree
	w.lon += east / (metersPerDegree * math.Cos(w.lat*math.Pi/180))
	w.reflect()
	return w.Position()
}

// reflect mirrors the position back into the area and turns the heading with it. A step longer
// than the area is mirrored again; the clamp is only a guard against pathological sizes.
func (w *Walker) reflect() {
	for i := 0; i < 8; i++ {
		switch {
		case w.lat < w.area.MinLat:
			w.lat = 2*w.area.MinLat - w.lat
			w.heading = math.Pi - w.heading
		case w.lat > w.area.MaxLat:
			w.lat = 2*w.area.MaxLat - w.lat
			w.heading = math.Pi - w.heading
		case w.lon < w.area.MinLon:
			w.lon = 2*w.area.MinLon - w.lon
			w.heading = -w.heading
		case w.lon > w.area.MaxLon:
			w.lon = 2*w.area.MaxLon - w.lon
			w.heading = -w.heading
		default:
			return
		}
	}
	w.lat = math.Min(math.Max(w.lat, w.area.MinLat), w.area.MaxLat)
	w.lon = math.Min(math.Max(w.lon, w.area.MinLon), w.area.MaxLon)
}

func round(v float64) float64 {
	return math.Round(v*coordinateScale) / coordinateScale
}
