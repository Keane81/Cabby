package grpcserver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Keane81/Cabby/backend/contracts/locationpb"
	"google.golang.org/grpc/status"
)

// The coordinates one run is made of. They are long and distinct on purpose: a short value would be
// a substring of a timestamp and the guard would report a leak where nothing leaked.
const (
	leakLatitude   = 12.3456789
	leakLongitude  = 98.7654321
	leakLatText    = "12.3456789"
	leakLonText    = "98.7654321"
	leakOverLat    = 190.1234567
	leakOverLatTxt = "190.1234567"
)

// TestNoSinkOfLocationCarriesACoordinate is FR-011 and SC-006 on the internal interface: after an
// accepted call, a rejected one and one that failed in storage, what the service writes down — its
// log and its metrics — and what a refusal says, name a request by its method and its outcome and
// repeat no coordinate.
func TestNoSinkOfLocationCarriesACoordinate(t *testing.T) {
	storage := &memoryLocations{}
	server := serveWith(t, storage)

	var refusals []string
	refused := func(desc string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: the call succeeded, want a refusal", desc)
		}
		refusals = append(refusals, status.Convert(err).String())
	}

	if _, err := server.RecordCabberLocation(context.Background(), &locationpb.RecordCabberLocationRequest{
		CabberId: testCabber, Latitude: leakLatitude, Longitude: leakLongitude,
	}); err != nil {
		t.Fatalf("accepted call: %v", err)
	}
	_, err := server.RecordCabberLocation(context.Background(), &locationpb.RecordCabberLocationRequest{
		CabberId: testCabber, Latitude: leakOverLat, Longitude: leakLongitude,
	})
	refused("a latitude out of range", err)

	storage.err = errors.New("insert cabber_location: row (" + leakLatText + ", " + leakLonText + ") failed")
	_, err = server.RecordCabberLocation(context.Background(), &locationpb.RecordCabberLocationRequest{
		CabberId: testCabber, Latitude: leakLatitude, Longitude: leakLongitude,
	})
	refused("a storage failure", err)

	sinks := []struct{ name, text string }{
		{"the log", server.logs.String()},
		{"the metrics", server.exported(t)},
	}
	for index, refusal := range refusals {
		sinks = append(sinks, struct{ name, text string }{"refusal " + string(rune('1'+index)), refusal})
	}
	for _, sink := range sinks {
		for _, value := range []string{leakLatText, leakLonText, leakOverLatTxt, "12.34567", "98.76543", "190.12345"} {
			if strings.Contains(sink.text, value) {
				t.Errorf("%s repeats a coordinate: %s", sink.name, sink.text)
			}
		}
	}
	for _, sink := range sinks[:2] {
		for _, name := range []string{"latitude", "longitude"} {
			if strings.Contains(sink.text, name) {
				t.Errorf("%s names the field %q: %s", sink.name, name, sink.text)
			}
		}
	}
}
