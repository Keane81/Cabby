package route

import (
	"math"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/emulator/internal/config"
)

// haversine is the reference distance in metres, independent of the walker's own projection.
func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const earth = 6_371_000.0
	rad := math.Pi / 180
	dLat, dLon := (lat2-lat1)*rad, (lon2-lon1)*rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earth * math.Asin(math.Sqrt(a))
}

func inside(a config.Area, lat, lon float64) bool {
	return lat >= a.MinLat && lat <= a.MaxLat && lon >= a.MinLon && lon <= a.MaxLon
}

func TestWalkStaysInsideAreaForManySteps(t *testing.T) {
	area := config.DefaultArea
	for _, interval := range []time.Duration{time.Second, 5 * time.Second, 10 * time.Minute} {
		for index := 0; index < 5; index++ {
			w := New(area, 42, index)
			lat, lon := w.Position()
			if !inside(area, lat, lon) {
				t.Fatalf("start (%v,%v) outside the area", lat, lon)
			}
			for step := 0; step < 100_000/5; step++ {
				lat, lon = w.Step(interval)
				if !inside(area, lat, lon) {
					t.Fatalf("interval %s index %d step %d: (%v,%v) outside the area", interval, index, step, lat, lon)
				}
			}
		}
	}
}

func TestWalkStaysInsideASmallArea(t *testing.T) {
	// One kilometre square: the border is met all the time.
	area := config.Area{MinLat: 55.0, MinLon: 37.0, MaxLat: 55.0091, MaxLon: 37.0159}
	w := New(area, 7, 3)
	for step := 0; step < 100_000; step++ {
		if lat, lon := w.Step(5 * time.Second); !inside(area, lat, lon) {
			t.Fatalf("step %d: (%v,%v) outside", step, lat, lon)
		}
	}
}

func TestNeighbourDistanceNeverExceedsSpeedTimesInterval(t *testing.T) {
	area := config.DefaultArea
	interval := 5 * time.Second
	for index := 0; index < 20; index++ {
		w := New(area, 99, index)
		limit := w.SpeedMS()*interval.Seconds()*1.002 + 0.05 // projection and rounding tolerance
		prevLat, prevLon := w.Position()
		for step := 0; step < 5000; step++ {
			lat, lon := w.Step(interval)
			if d := haversine(prevLat, prevLon, lat, lon); d > limit {
				t.Fatalf("index %d step %d: moved %.2f m, limit %.2f m", index, step, d, limit)
			}
			prevLat, prevLon = lat, lon
		}
	}
}

func TestSpeedIsAnUrbanOne(t *testing.T) {
	for index := 0; index < 200; index++ {
		kmh := New(config.DefaultArea, 5, index).SpeedMS() * 3.6
		if kmh < 20 || kmh > 50 {
			t.Fatalf("index %d: %.1f km/h outside 20–50", index, kmh)
		}
	}
}

func TestSameSeedAndIndexReproduce(t *testing.T) {
	a, b := New(config.DefaultArea, 1234, 17), New(config.DefaultArea, 1234, 17)
	for step := 0; step < 1000; step++ {
		latA, lonA := a.Step(5 * time.Second)
		latB, lonB := b.Step(5 * time.Second)
		if latA != latB || lonA != lonB {
			t.Fatalf("step %d differs: (%v,%v) vs (%v,%v)", step, latA, lonA, latB, lonB)
		}
	}
}

func TestDifferentIndicesAndSeedsDiffer(t *testing.T) {
	area := config.DefaultArea
	lat0, lon0 := New(area, 1, 0).Position()
	lat1, lon1 := New(area, 1, 1).Position()
	lat2, lon2 := New(area, 2, 0).Position()
	if (lat0 == lat1 && lon0 == lon1) || (lat0 == lat2 && lon0 == lon2) {
		t.Error("different index or seed produced the same start")
	}
}

func TestBorderReflectsHeadingInsteadOfSticking(t *testing.T) {
	area := config.Area{MinLat: 55.0, MinLon: 37.0, MaxLat: 55.1, MaxLon: 37.2}

	// A few metres from the northern border, driving straight at it: the first step crosses it
	// and the heading must turn to point away.
	w := New(area, 1, 0)
	w.lat, w.lon, w.heading = area.MaxLat-0.00001, 37.1, 0
	lat, _ := w.Step(time.Second)
	if lat > area.MaxLat {
		t.Fatalf("crossed the northern border: %v", lat)
	}
	if math.Cos(w.heading) > 0.6 {
		t.Errorf("heading %v still points north after the reflection", w.heading)
	}

	// And a long drive along the border never leaves the area.
	for step := 0; step < 200; step++ {
		if lat, lon := w.Step(5 * time.Second); !inside(area, lat, lon) {
			t.Fatalf("step %d left the area: (%v,%v)", step, lat, lon)
		}
	}
}

func TestLongStepIsStillInsideTheArea(t *testing.T) {
	area := config.Area{MinLat: 55.0, MinLon: 37.0, MaxLat: 55.0091, MaxLon: 37.0159}
	w := New(area, 3, 1)
	for step := 0; step < 1000; step++ {
		if lat, lon := w.Step(30 * time.Minute); !inside(area, lat, lon) {
			t.Fatalf("step %d outside: (%v,%v)", step, lat, lon)
		}
	}
}

func TestCoordinatesAreRoundedToSevenDecimals(t *testing.T) {
	w := New(config.DefaultArea, 8, 8)
	for step := 0; step < 100; step++ {
		lat, lon := w.Step(5 * time.Second)
		for _, v := range []float64{lat, lon} {
			if scaled := v * 1e7; math.Abs(scaled-math.Round(scaled)) > 1e-3 {
				t.Fatalf("%v has more than seven decimals", v)
			}
		}
	}
}
