package authclient

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/requestid"
	"github.com/Keane81/Cabby/backend/contracts/authpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// TestCallCarriesTheDeadlineOfResearch: every call leaves with the 2-second budget of R-09
// attached to its context, whatever the caller brought.
func TestCallCarriesTheDeadlineOfResearch(t *testing.T) {
	service := &fakeService{}
	client := connect(t, service)

	if _, err := client.RegisterCabber(context.Background(), "Иван", "ivan@example.com", "1234"); err != nil {
		t.Fatalf("RegisterCabber = %v", err)
	}
	if !service.sawDeadline {
		t.Fatal("the call reached the service without a deadline")
	}
	if remaining := time.Until(service.until); remaining <= 0 || remaining > CallTimeout {
		t.Errorf("the deadline left %v, want the window of %v", remaining, CallTimeout)
	}
}

// TestTheRequestIDTravelsWithEveryCall is CHK034 on the sending side: the identifier the public
// server minted reaches auth under the key the two modules fix, and a call made outside that server
// sends no identifier rather than one it invented.
func TestTheRequestIDTravelsWithEveryCall(t *testing.T) {
	service := &fakeService{}
	client := connect(t, service)
	id := requestid.New()
	ctx := requestid.Into(context.Background(), id)

	if _, err := client.RegisterCabber(ctx, "Иван", "ivan@example.com", "1234"); err != nil {
		t.Fatalf("RegisterCabber: %v", err)
	}
	if _, err := client.CreateCabberSession(ctx, "ivan@example.com", "1234"); err != nil {
		t.Fatalf("CreateCabberSession: %v", err)
	}
	if err := client.DeleteCabberSession(ctx, "opaque-token"); err != nil {
		t.Fatalf("DeleteCabberSession: %v", err)
	}
	if _, err := client.RegisterCabber(context.Background(), "Иван", "ivan@example.com", "1234"); err != nil {
		t.Fatalf("RegisterCabber without an identifier: %v", err)
	}

	if len(service.sawRequestIDs) != 4 {
		t.Fatalf("the service recorded %d calls, want 4", len(service.sawRequestIDs))
	}
	for index, seen := range service.sawRequestIDs[:3] {
		if seen != id {
			t.Errorf("call %d carried %q, want the identifier %q", index, seen, id)
		}
	}
	if service.sawRequestIDs[3] != "" {
		t.Errorf("a request that brought no identifier sent %q", service.sawRequestIDs[3])
	}
}

// TestSuccessfulCallBecomesADomainResult keeps no protobuf type on the far side of the port.
func TestSuccessfulCallBecomesADomainResult(t *testing.T) {
	client := connect(t, &fakeService{})
	expires := time.Unix(1_800_000_000, 0).UTC()

	cabber, err := client.RegisterCabber(context.Background(), "Иван", "ivan@example.com", "1234")
	if err != nil || cabber.ID != "cabber-1" || cabber.Email != "ivan@example.com" {
		t.Errorf("RegisterCabber = %+v, %v", cabber, err)
	}
	session, err := client.CreateCabberSession(context.Background(), "ivan@example.com", "1234")
	if err != nil || session.AccessToken != "opaque-token" || !session.ExpiresAt.Equal(expires) {
		t.Errorf("CreateCabberSession = %+v, %v", session, err)
	}
	if err := client.DeleteCabberSession(context.Background(), "opaque-token"); err != nil {
		t.Errorf("DeleteCabberSession = %v", err)
	}
}

// TestSlowAndUnreachableServiceBothReportDependency is the domain result a caller turns into 503:
// neither a service that stops answering nor one that refuses the call may look like a rejection
// of the cabber (FR-016, R-09).
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

		_, err := client.CreateCabberSession(context.Background(), "ivan@example.com", "1234")
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("%s: CreateCabberSession = %v, want ErrUnavailable", tc.desc, err)
		}
		if errors.Is(err, ErrUnauthorized) {
			t.Errorf("%s: a dependency failure was reported as a rejected access", tc.desc)
		}
	}
}

// TestRegistrationIsNeverRetried guards R-09: a second attempt could create a second account, so
// one call is all the client is allowed to make.
func TestRegistrationIsNeverRetried(t *testing.T) {
	service := &fakeService{failWith: status.Error(codes.Unavailable, "connection reset")}
	client := connect(t, service)

	if _, err := client.RegisterCabber(context.Background(), "Иван", "ivan@example.com", "1234"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("RegisterCabber = %v, want ErrUnavailable", err)
	}
	if service.calls != 1 {
		t.Errorf("the service was called %d times, want exactly 1", service.calls)
	}
}

// TestStatusTranslation pins the table of data-model §6 on the side of the transport, including
// the ErrorField detail the service attaches to a validation defect.
func TestStatusTranslation(t *testing.T) {
	withField := status.New(codes.InvalidArgument, "bad name")
	withField, err := withField.WithDetails(&authpb.ErrorField{Field: "name", Reason: "too_long"})
	if err != nil {
		t.Fatalf("attach ErrorField: %v", err)
	}
	invented := status.New(codes.InvalidArgument, "bad field")
	invented, err = invented.WithDetails(&authpb.ErrorField{Field: "password_hash", Reason: "too_long"})
	if err != nil {
		t.Fatalf("attach ErrorField: %v", err)
	}

	for _, tc := range []struct {
		desc string
		got  error
		want any
	}{
		{"no failure", nil, nil},
		{"field of the contract", withField.Err(), Invalid{Field: "name", Reason: "too_long"}},
		{"field we do not publish", invented.Err(), Invalid{}},
		{"no detail", status.Error(codes.InvalidArgument, "no detail"), Invalid{}},
		{"taken", status.Error(codes.AlreadyExists, "duplicate key value"), ErrEmailTaken},
		{"rejected access", status.Error(codes.Unauthenticated, "no such access"), ErrUnauthorized},
		{"down", status.Error(codes.Unavailable, "ready: false"), ErrUnavailable},
		{"too slow", status.Error(codes.DeadlineExceeded, "context deadline exceeded"), ErrUnavailable},
		{"defect of the service", status.Error(codes.Internal, "panic"), ErrInternal},
		{"not a status", errors.New("write: broken pipe"), ErrInternal},
	} {
		got := translate(tc.got)
		if !sameFailure(got, tc.want) {
			t.Errorf("%s: translate = %#v, want %#v", tc.desc, got, tc.want)
		}
	}
}

func sameFailure(got error, want any) bool {
	switch want := want.(type) {
	case nil:
		return got == nil
	case Invalid:
		invalid, ok := got.(Invalid)
		return ok && invalid == want
	case error:
		return errors.Is(got, want)
	}
	return false
}

// fakeService is the stand-in of auth.v1.AuthService: it records what the transport asked of it
// and answers however the test needs.
type fakeService struct {
	authpb.UnimplementedAuthServiceServer

	calls         int
	sawDeadline   bool
	until         time.Time
	hold          time.Duration
	failWith      error
	sawRequestIDs []string
}

func (f *fakeService) note(ctx context.Context) {
	f.calls++
	f.sawDeadline = false
	if until, ok := ctx.Deadline(); ok {
		f.sawDeadline, f.until = true, until
	}
	presented := metadata.ValueFromIncomingContext(ctx, requestid.MetadataKey)
	if len(presented) != 1 {
		presented = []string{""}
	}
	f.sawRequestIDs = append(f.sawRequestIDs, presented[0])
	if f.hold > 0 {
		time.Sleep(f.hold)
	}
}

func (f *fakeService) RegisterCabber(ctx context.Context, request *authpb.RegisterCabberRequest) (*authpb.RegisterCabberResponse, error) {
	f.note(ctx)
	if f.failWith != nil {
		return nil, f.failWith
	}
	return &authpb.RegisterCabberResponse{CabberId: "cabber-1", Email: request.GetEmail()}, nil
}

func (f *fakeService) CreateCabberSession(ctx context.Context, _ *authpb.CreateCabberSessionRequest) (*authpb.CreateCabberSessionResponse, error) {
	f.note(ctx)
	if f.failWith != nil {
		return nil, f.failWith
	}
	return &authpb.CreateCabberSessionResponse{
		AccessToken: "opaque-token", ExpiresAtUnix: time.Unix(1_800_000_000, 0).Unix(),
	}, nil
}

func (f *fakeService) DeleteCabberSession(ctx context.Context, _ *authpb.DeleteCabberSessionRequest) (*authpb.DeleteCabberSessionResponse, error) {
	f.note(ctx)
	if f.failWith != nil {
		return nil, f.failWith
	}
	return &authpb.DeleteCabberSessionResponse{}, nil
}

// connect serves the fake over an in-process listener and returns a client of the same shape the
// gateway builds with Dial.
func connect(t *testing.T, service authpb.AuthServiceServer) *Client {
	t.Helper()

	listener := bufconn.Listen(64 * 1024)
	server := grpc.NewServer()
	authpb.RegisterAuthServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///auth",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return &Client{conn: conn, api: authpb.NewAuthServiceClient(conn), timeout: CallTimeout}
}
