package service

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/location/internal/repo"
	"github.com/rs/zerolog"
)

const testCabber = "0b6f1e1c-6a2c-4c3e-9d7a-0a1b2c3d4e5f"

// memoryLocations is a repo.LocationRepository that keeps what it is given.
type memoryLocations struct {
	rows []repo.Location
	err  error
}

func (m *memoryLocations) Insert(_ context.Context, location repo.Location) error {
	if m.err != nil {
		return m.err
	}
	m.rows = append(m.rows, location)
	return nil
}

func newService(locations repo.LocationRepository, now time.Time) *Service {
	return New(locations, zerolog.New(io.Discard), func() time.Time { return now })
}

func TestRecordStoresOneRecordStampedByTheClock(t *testing.T) {
	storage := &memoryLocations{}
	clock := time.Date(2026, 10, 1, 9, 30, 12, 345678912, time.FixedZone("x", 3*3600))
	svc := newService(storage, clock)

	receivedAt, err := svc.Record(context.Background(), testCabber, 55.7558, 37.6173)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if len(storage.rows) != 1 {
		t.Fatalf("stored %d rows, want 1", len(storage.rows))
	}
	row := storage.rows[0]
	want := clock.UTC().Truncate(time.Microsecond)
	if !row.ReceivedAt.Equal(want) || !receivedAt.Equal(want) {
		t.Errorf("received_at: row %v, answer %v, want %v", row.ReceivedAt, receivedAt, want)
	}
	if row.CabberID != testCabber || row.Latitude != 55.7558 || row.Longitude != 37.6173 {
		t.Errorf("row = %+v", row)
	}
}

func TestRecordRejectionLeavesNoTrace(t *testing.T) {
	storage := &memoryLocations{}
	svc := newService(storage, time.Now())

	for _, tc := range []struct {
		desc     string
		cabber   string
		lat, lon float64
	}{
		{"latitude out of range", testCabber, 91, 0},
		{"longitude out of range", testCabber, 0, -181},
		{"cabber is not a UUID", "not-a-uuid", 1, 1},
		{"cabber is empty", "", 1, 1},
	} {
		if _, err := svc.Record(context.Background(), tc.cabber, tc.lat, tc.lon); err == nil {
			t.Errorf("%s: Record succeeded", tc.desc)
		}
	}
	if len(storage.rows) != 0 {
		t.Fatalf("rejected requests left %d rows", len(storage.rows))
	}
}

func TestRecordReportsAnInvalidOwnerApartFromAValidationFailure(t *testing.T) {
	svc := newService(&memoryLocations{}, time.Now())
	_, err := svc.Record(context.Background(), "nope", 1, 1)
	if !errors.Is(err, ErrInvalidOwner) {
		t.Fatalf("err = %v, want ErrInvalidOwner", err)
	}
}

func TestRecordAddsARecordPerCallAndKeepsThePreviousOnes(t *testing.T) {
	storage := &memoryLocations{}
	svc := newService(storage, time.Now())

	for range 3 {
		if _, err := svc.Record(context.Background(), testCabber, 10, 20); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	if len(storage.rows) != 3 {
		t.Fatalf("stored %d rows, want 3: equal positions are separate records", len(storage.rows))
	}
}

// TestRecordNeverAnswersTooFrequent is FR-007: however fast the calls come, none is refused for its
// frequency, and the case has no error that could say so.
func TestRecordNeverAnswersTooFrequent(t *testing.T) {
	storage := &memoryLocations{}
	moment := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	svc := newService(storage, moment)

	for range 500 {
		if _, err := svc.Record(context.Background(), testCabber, 1, 1); err != nil {
			t.Fatalf("Record at an unchanged instant: %v", err)
		}
	}
	if len(storage.rows) != 500 {
		t.Fatalf("stored %d rows, want 500", len(storage.rows))
	}
}

func TestRecordMapsStorageFailureToDependency(t *testing.T) {
	storage := &memoryLocations{err: errors.New(`insert cabber_location: value "55.7558" violates something`)}
	svc := newService(storage, time.Now())

	_, err := svc.Record(context.Background(), testCabber, 55.7558, 37.6173)
	if !errors.Is(err, ErrDependency) {
		t.Fatalf("err = %v, want ErrDependency", err)
	}
	if got := err.Error(); got != ErrDependency.Error() {
		t.Errorf("the error carries the cause: %q", got)
	}
}
