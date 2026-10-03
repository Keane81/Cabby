package grpcserver

import (
	"bytes"
	"context"
	"errors"
	"github.com/Keane81/Cabby/backend/platform/requestid"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/contracts/locationpb"
	"github.com/Keane81/Cabby/backend/location/internal/metrics"
	"github.com/Keane81/Cabby/backend/location/internal/repo"
	"github.com/Keane81/Cabby/backend/location/internal/service"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const testCabber = "0b6f1e1c-6a2c-4c3e-9d7a-0a1b2c3d4e5f"

// memoryLocations is a repo.LocationRepository that keeps what it is given or fails on demand.
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

// fixture is the transport under test together with the metrics its interceptor records.
type fixture struct {
	locationpb.LocationServiceClient
	raw     *grpc.ClientConn
	metrics *metrics.Metrics
	logs    *bytes.Buffer
}

func serveWith(t *testing.T, storage repo.LocationRepository) *fixture {
	t.Helper()

	var logs bytes.Buffer
	listener := bufconn.Listen(64 * 1024)
	metrics := metrics.New()
	logger := zerolog.New(&logs)
	server := grpc.NewServer(grpc.UnaryInterceptor(metrics.UnaryInterceptor(logger)))
	NewServer(service.New(metrics.Locations(storage), logger, time.Now)).Register(server)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///location",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial the in-process listener: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return &fixture{
		LocationServiceClient: locationpb.NewLocationServiceClient(conn),
		raw:                   conn, metrics: metrics, logs: &logs,
	}
}

func (f *fixture) exported(t *testing.T) string {
	t.Helper()
	response := httptest.NewRecorder()
	f.metrics.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /metrics = %d", response.Code)
	}
	return response.Body.String()
}

func (f *fixture) assertCount(t *testing.T, expected string) {
	t.Helper()
	if body := f.exported(t); !strings.Contains(body, expected) {
		t.Fatalf("missing %q in\n%s", expected, body)
	}
}

func TestRecordCabberLocationStoresTheRecordAndAnswersItsTime(t *testing.T) {
	storage := &memoryLocations{}
	server := serveWith(t, storage)

	before := time.Now().Add(-time.Second)
	answer, err := server.RecordCabberLocation(context.Background(), &locationpb.RecordCabberLocationRequest{
		CabberId: testCabber, Latitude: 55.7558, Longitude: 37.6173,
	})
	if err != nil {
		t.Fatalf("RecordCabberLocation: %v", err)
	}
	if len(storage.rows) != 1 {
		t.Fatalf("stored %d rows, want 1", len(storage.rows))
	}
	got := time.Unix(answer.GetReceivedAtUnix(), int64(answer.GetReceivedAtNanos()))
	if got.Before(before) || !got.Equal(storage.rows[0].ReceivedAt) {
		t.Errorf("answered %v, stored %v", got, storage.rows[0].ReceivedAt)
	}
	server.assertCount(t, `cabby_location_requests_total{outcome="success"} 1`)
	server.assertCount(t, `cabby_location_records_total{outcome="success"} 1`)
}

// TestRecordCabberLocationNamesTheFieldAtFault is the transport half of FR-002 and FR-003: the field
// travels in ErrorField details, and nothing is stored.
func TestRecordCabberLocationNamesTheFieldAtFault(t *testing.T) {
	storage := &memoryLocations{}
	server := serveWith(t, storage)

	for _, tc := range []struct {
		desc       string
		request    *locationpb.RecordCabberLocationRequest
		wantField  string
		wantReason string
	}{
		{"latitude over", &locationpb.RecordCabberLocationRequest{CabberId: testCabber, Latitude: 90.5}, "latitude", "out_of_range"},
		{"longitude under", &locationpb.RecordCabberLocationRequest{CabberId: testCabber, Longitude: -181}, "longitude", "out_of_range"},
	} {
		_, err := server.RecordCabberLocation(context.Background(), tc.request)
		if got := status.Code(err); got != codes.InvalidArgument {
			t.Errorf("%s: code = %v, want INVALID_ARGUMENT", tc.desc, got)
			continue
		}
		details := status.Convert(err).Details()
		if len(details) != 1 {
			t.Errorf("%s: %d details, want one ErrorField", tc.desc, len(details))
			continue
		}
		defect, ok := details[0].(*locationpb.ErrorField)
		if !ok || defect.GetField() != tc.wantField || defect.GetReason() != tc.wantReason {
			t.Errorf("%s: detail = %v, want %s/%s", tc.desc, details[0], tc.wantField, tc.wantReason)
		}
	}
	if len(storage.rows) != 0 {
		t.Fatalf("rejections stored %d rows", len(storage.rows))
	}
	server.assertCount(t, `cabby_location_requests_total{outcome="invalid_argument"} 2`)
}

func TestRecordCabberLocationReportsStorageAsUnavailable(t *testing.T) {
	storage := &memoryLocations{err: errors.New(`insert cabber_location: latitude 55.7558 broke it`)}
	server := serveWith(t, storage)

	_, err := server.RecordCabberLocation(context.Background(), &locationpb.RecordCabberLocationRequest{
		CabberId: testCabber, Latitude: 55.7558, Longitude: 37.6173,
	})
	if got := status.Code(err); got != codes.Unavailable {
		t.Fatalf("code = %v, want UNAVAILABLE", got)
	}
	if got := status.Convert(err).Message(); got != msgStorageGone {
		t.Errorf("message = %q, want the fixed %q", got, msgStorageGone)
	}
	server.assertCount(t, `cabby_location_records_total{outcome="failure"} 1`)
}

// TestRecordCabberLocationAnswersInternalForAnInvalidCabber: an identifier that is not a UUID is a
// defect of the gateway, not of the client, so it must not look like a client error.
func TestRecordCabberLocationAnswersInternalForAnInvalidCabber(t *testing.T) {
	server := serveWith(t, &memoryLocations{})

	for _, cabber := range []string{"", "not-a-uuid"} {
		_, err := server.RecordCabberLocation(context.Background(), &locationpb.RecordCabberLocationRequest{
			CabberId: cabber, Latitude: 1, Longitude: 1,
		})
		if got := status.Code(err); got != codes.Internal {
			t.Errorf("cabber %q: code = %v, want INTERNAL", cabber, got)
		}
		if len(status.Convert(err).Details()) != 0 {
			t.Errorf("cabber %q: the status carries details", cabber)
		}
	}
}

func TestRequestIDOfTheGatewayIsReusedWhenWellFormedAndReplacedOtherwise(t *testing.T) {
	for _, tc := range []struct {
		desc, sent string
		reused     bool
	}{
		{"well formed", "0123456789abcdef", true},
		{"too short", "abc", false},
		{"uppercase", "0123456789ABCDEF", false},
		{"text of a caller's choosing", "55.7558,37.6173!!!!", false},
	} {
		server := serveWith(t, &memoryLocations{})
		ctx := metadata.AppendToOutgoingContext(context.Background(), requestid.MetadataKey, tc.sent)
		if _, err := server.RecordCabberLocation(ctx, &locationpb.RecordCabberLocationRequest{
			CabberId: testCabber, Latitude: 1, Longitude: 1,
		}); err != nil {
			t.Fatalf("%s: %v", tc.desc, err)
		}
		logged := server.logs.String()
		if got := strings.Contains(logged, `"request_id":"`+tc.sent+`"`); got != tc.reused {
			t.Errorf("%s: request_id reused = %v, want %v in %s", tc.desc, got, tc.reused, logged)
		}
	}
}

// TestUnknownMethodKeepsTheLogAndMetricSetClosed: a call the service does not serve adds no series
// and puts no text of the caller's into a log line.
func TestUnknownMethodKeepsTheLogAndMetricSetClosed(t *testing.T) {
	server := serveWith(t, &memoryLocations{})
	err := server.raw.Invoke(context.Background(), "/location.v1.LocationService/NotInContract",
		&locationpb.RecordCabberLocationRequest{}, &locationpb.RecordCabberLocationResponse{})
	if got := status.Code(err); got != codes.Unimplemented {
		t.Fatalf("an unknown method = %v, want UNIMPLEMENTED", got)
	}
	if strings.Contains(server.exported(t), "NotInContract") || strings.Contains(server.logs.String(), "NotInContract") {
		t.Fatal("an unknown method reached a metric or a log line")
	}
}
