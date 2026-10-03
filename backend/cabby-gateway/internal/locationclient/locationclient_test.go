package locationclient

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/contracts/locationpb"
	"github.com/Keane81/Cabby/backend/platform/requestid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const cabber = "0b6f1e1c-6a2c-4c3e-9d7a-0a1b2c3d4e5f"

func TestCallCarriesTheDeadlineAndTheRequestID(t *testing.T) {
	service := &fakeService{}
	client := connect(t, service)
	id := requestid.New()

	if _, err := client.RecordCabberLocation(requestid.Into(context.Background(), id), cabber, 55.7558, 37.6173); err != nil {
		t.Fatalf("RecordCabberLocation: %v", err)
	}
	if !service.sawDeadline {
		t.Fatal("the call reached the service without a deadline")
	}
	if remaining := time.Until(service.until); remaining <= 0 || remaining > CallTimeout {
		t.Errorf("the deadline left %v, want the window of %v", remaining, CallTimeout)
	}
	if service.sawRequestID != id {
		t.Errorf("the service saw request id %q, want %q", service.sawRequestID, id)
	}

	// A call made outside the public server sends no identifier rather than one it invented.
	if _, err := client.RecordCabberLocation(context.Background(), cabber, 1, 1); err != nil {
		t.Fatalf("RecordCabberLocation: %v", err)
	}
	if service.sawRequestID != "" {
		t.Errorf("a call without an identifier sent %q", service.sawRequestID)
	}
}

func TestSuccessfulCallBecomesTheTimeOfReceipt(t *testing.T) {
	client := connect(t, &fakeService{})

	receivedAt, err := client.RecordCabberLocation(context.Background(), cabber, 55.7558, 37.6173)
	if err != nil {
		t.Fatalf("RecordCabberLocation: %v", err)
	}
	if want := time.Unix(1_800_000_000, 345678000).UTC(); !receivedAt.Equal(want) || receivedAt.Location() != time.UTC {
		t.Errorf("received at %v, want %v in UTC", receivedAt, want)
	}
}

func TestTheCoordinatesAndTheCabberReachTheService(t *testing.T) {
	service := &fakeService{}
	client := connect(t, service)

	if _, err := client.RecordCabberLocation(context.Background(), cabber, -12.5, 100.25); err != nil {
		t.Fatalf("RecordCabberLocation: %v", err)
	}
	if service.saw.GetCabberId() != cabber || service.saw.GetLatitude() != -12.5 || service.saw.GetLongitude() != 100.25 {
		t.Errorf("the service saw %v", service.saw)
	}
}

// TestSlowAndUnreachableServiceBothReportDependency: neither a service that stops answering nor one
// that refuses the call may look like a rejection of the coordinates.
func TestSlowAndUnreachableServiceBothReportDependency(t *testing.T) {
	for _, tc := range []struct {
		desc    string
		service *fakeService
	}{
		{"answers after the deadline", &fakeService{hold: 200 * time.Millisecond}},
		{"refuses the call", &fakeService{failWith: status.Error(codes.Unavailable, "connection refused")}},
	} {
		client := connect(t, tc.service)
		client.timeout = 50 * time.Millisecond

		_, err := client.RecordCabberLocation(context.Background(), cabber, 1, 1)
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("%s: RecordCabberLocation = %v, want ErrUnavailable", tc.desc, err)
		}
		var invalid Invalid
		if errors.As(err, &invalid) {
			t.Errorf("%s: a dependency failure was reported as a rejection", tc.desc)
		}
	}
}

// TestRecordIsNeverRetried guards research R-06: a second attempt after a commit would add a second
// record the client never asked for, so one call is all the client is allowed to make.
func TestRecordIsNeverRetried(t *testing.T) {
	service := &fakeService{failWith: status.Error(codes.Unavailable, "connection reset")}
	client := connect(t, service)

	if _, err := client.RecordCabberLocation(context.Background(), cabber, 1, 1); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("RecordCabberLocation = %v, want ErrUnavailable", err)
	}
	if service.calls != 1 {
		t.Errorf("the service was called %d times, want exactly 1", service.calls)
	}
}

func TestStatusTranslation(t *testing.T) {
	withField, err := status.New(codes.InvalidArgument, "bad latitude 91.5").
		WithDetails(&locationpb.ErrorField{Field: "latitude", Reason: "out_of_range"})
	if err != nil {
		t.Fatalf("attach ErrorField: %v", err)
	}
	invented, err := status.New(codes.InvalidArgument, "bad field").
		WithDetails(&locationpb.ErrorField{Field: "cabber_id", Reason: "out_of_range"})
	if err != nil {
		t.Fatalf("attach ErrorField: %v", err)
	}

	for _, tc := range []struct {
		desc string
		got  error
		want any
	}{
		{"field of the contract", withField.Err(), Invalid{Field: "latitude", Reason: "out_of_range"}},
		{"field we do not publish", invented.Err(), Invalid{}},
		{"no detail", status.Error(codes.InvalidArgument, "no detail"), Invalid{}},
		{"down", status.Error(codes.Unavailable, "ready: false"), ErrUnavailable},
		{"too slow", status.Error(codes.DeadlineExceeded, "context deadline exceeded"), ErrUnavailable},
		{"cancelled", status.Error(codes.Canceled, "gone"), ErrUnavailable},
		{"defect of the service", status.Error(codes.Internal, "panic"), ErrInternal},
		{"unexpected refusal", status.Error(codes.Unauthenticated, "who"), ErrInternal},
		{"not a status", errors.New("write: broken pipe"), ErrInternal},
	} {
		got := translate(tc.got)
		switch want := tc.want.(type) {
		case Invalid:
			if invalid, ok := got.(Invalid); !ok || invalid != want {
				t.Errorf("%s: translate = %#v, want %#v", tc.desc, got, want)
			}
		case error:
			if !errors.Is(got, want) {
				t.Errorf("%s: translate = %#v, want %v", tc.desc, got, want)
			}
		}
		if strings.Contains(got.Error(), "91.5") {
			t.Errorf("%s: the translation repeats a value of the request: %v", tc.desc, got)
		}
	}
}

// fakeService is the stand-in of location.v1.LocationService.
type fakeService struct {
	locationpb.UnimplementedLocationServiceServer

	calls        int
	sawDeadline  bool
	until        time.Time
	sawRequestID string
	saw          *locationpb.RecordCabberLocationRequest
	hold         time.Duration
	failWith     error
}

func (f *fakeService) RecordCabberLocation(ctx context.Context, request *locationpb.RecordCabberLocationRequest) (*locationpb.RecordCabberLocationResponse, error) {
	f.calls++
	f.saw = request
	f.sawDeadline = false
	if until, ok := ctx.Deadline(); ok {
		f.sawDeadline, f.until = true, until
	}
	f.sawRequestID = ""
	if presented := metadata.ValueFromIncomingContext(ctx, requestid.MetadataKey); len(presented) == 1 {
		f.sawRequestID = presented[0]
	}
	if f.hold > 0 {
		time.Sleep(f.hold)
	}
	if f.failWith != nil {
		return nil, f.failWith
	}
	return &locationpb.RecordCabberLocationResponse{ReceivedAtUnix: 1_800_000_000, ReceivedAtNanos: 345678000}, nil
}

// connect serves the fake over an in-process listener and returns a client of the same shape the
// gateway builds with Dial.
func connect(t *testing.T, service locationpb.LocationServiceServer) *Client {
	t.Helper()

	listener := bufconn.Listen(64 * 1024)
	server := grpc.NewServer()
	locationpb.RegisterLocationServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///location",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return &Client{conn: conn, api: locationpb.NewLocationServiceClient(conn), timeout: CallTimeout}
}
