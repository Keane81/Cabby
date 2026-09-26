package grpcserver

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/auth/internal/repo"
	"github.com/Keane81/Cabby/backend/auth/internal/service"
	"github.com/Keane81/Cabby/backend/contracts/authpb"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// TestUnknownMethodKeepsLabelSetClosed guards the fixed cardinality of R-11: a call the service
// does not serve adds no metric series.
func TestUnknownMethodKeepsLabelSetClosed(t *testing.T) {
	server := serve(t)
	err := server.raw.Invoke(context.Background(), "/auth.v1.AuthService/NotInContract",
		&authpb.DeleteCabberSessionRequest{}, &authpb.DeleteCabberSessionResponse{})
	if got := status.Code(err); got != codes.Unimplemented {
		t.Fatalf("an unknown method = %v, want UNIMPLEMENTED", got)
	}
	if body := server.exported(t); strings.Contains(body, "NotInContract") {
		t.Fatalf("an unknown method added a metric series:\n%s", body)
	}
}

// fixture is the transport under test together with the metrics its interceptor records.
type fixture struct {
	authpb.AuthServiceClient
	raw     *grpc.ClientConn
	metrics *Metrics
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

// serve runs the transport over an in-process listener without any storage: enough to reach a
// method, never to complete it.
func serve(t *testing.T) *fixture {
	t.Helper()
	return serveWith(t, nil, nil)
}

// serveWith runs the transport over the storage stand-ins a test chooses. The clock is real here:
// a transport test asserts which status a case produces, while the moment a case reads the clock
// is the business of internal/service (R-10).
func serveWith(t *testing.T, cabbers repo.CabberRepository, sessions repo.SessionRepository) *fixture {
	t.Helper()
	return servingLogged(t, io.Discard, cabbers, sessions)
}

// servingLogged is serveWith for a test that reads back the lines the transport writes — the cases
// and the interceptor log into the same sink, which is what a guard on what leaves the process has to
// look at.
func servingLogged(t *testing.T, logs io.Writer, cabbers repo.CabberRepository, sessions repo.SessionRepository) *fixture {
	t.Helper()

	listener := bufconn.Listen(64 * 1024)
	metrics := NewMetrics()
	logger := zerolog.New(logs)
	server := grpc.NewServer(grpc.UnaryInterceptor(metrics.UnaryInterceptor(logger)))
	NewServer(service.New(cabbers, sessions, cheap, logger, time.Now)).Register(server)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///auth",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial the in-process listener: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return &fixture{AuthServiceClient: authpb.NewAuthServiceClient(conn), raw: conn, metrics: metrics}
}
