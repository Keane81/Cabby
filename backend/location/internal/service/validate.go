package service

import "math"

// Bounds of the coordinates in degrees, inclusive (spec 004 FR-002).
const (
	MaxLatitude  = 90.0
	MaxLongitude = 180.0

	// precision is the number of decimals kept: 1e-7 degree is about a centimetre, which loses
	// nothing a position report can use and keeps the stored values bounded (research R-05).
	precision = 1e7
)

// Validate checks a pair of coordinates and returns it rounded to the stored precision. Latitude is
// judged before longitude, so a request with two defects always gets the same answer. The checks are
// inclusive at the bounds, and 0, 0 is a position like any other.
func Validate(latitude, longitude float64) (float64, float64, error) {
	if err := check(FieldLatitude, latitude, MaxLatitude); err != nil {
		return 0, 0, err
	}
	if err := check(FieldLongitude, longitude, MaxLongitude); err != nil {
		return 0, 0, err
	}
	return round(latitude), round(longitude), nil
}

func check(field Field, value, limit float64) error {
	switch {
	case math.IsNaN(value) || math.IsInf(value, 0):
		return Validation{Field: field, Reason: ReasonNotANumber}
	case value < -limit || value > limit:
		return Validation{Field: field, Reason: ReasonOutOfRange}
	}
	return nil
}

// round keeps seven decimals. Negative zero is turned into zero so equal positions are stored equal.
func round(value float64) float64 {
	rounded := math.Round(value*precision) / precision
	if rounded == 0 {
		return 0
	}
	return rounded
}
