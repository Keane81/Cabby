package grpcserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/Keane81/Cabby/backend/contracts/authpb"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// requestIDShape is the shape the gateway draws in: 16 hex characters.
var requestIDShape = regexp.MustCompile(`^[0-9a-f]{16}$`)

// TestCallKeepsTheIdentifierTheGatewayMinted is the receiving half of CHK034: the value that leaves
// the gateway in metadata is the value the line of auth names the request by, which is what lets one
// search cover both services.
func TestCallKeepsTheIdentifierTheGatewayMinted(t *testing.T) {
	var logs bytes.Buffer
	intercept(t, &logs, incoming("0123456789abcdef"))

	if got := loggedID(t, &logs); got != "0123456789abcdef" {
		t.Errorf("request_id = %q, want the identifier of the call", got)
	}
}

// TestCallWithoutAUsableIdentifierDrawsItsOwn guards the log line against its input. The identifier
// is the only value of a request this service writes down, so anything that is not the fixed shape is
// replaced rather than repeated — and a call that brought none still gets one, or the two services
// have nothing to join their lines by.
func TestCallWithoutAUsableIdentifierDrawsItsOwn(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		values []string
	}{
		{"no metadata at all", nil},
		{"empty value", []string{""}},
		{"one character short", []string{"0123456789abcde"}},
		{"one character too long", []string{"0123456789abcdef0"}},
		{"not hexadecimal", []string{"0123456789abcdeg"}},
		{"uppercase", []string{"0123456789ABCDEF"}},
		{"two values at once", []string{"0123456789abcdef", "0123456789abcdef"}},
		{"text of a caller", []string{"auth request\n\"level\":\"info\""}},
	} {
		var logs bytes.Buffer
		intercept(t, &logs, incoming(tc.values...))

		id := loggedID(t, &logs)
		if !requestIDShape.MatchString(id) {
			t.Errorf("%s: request_id = %q, want 16 hex characters of our own", tc.desc, id)
		}
		for _, value := range tc.values {
			if id == value {
				t.Errorf("%s: the identifier of the caller reached the log: %q", tc.desc, id)
			}
		}
	}
}

// TestTheMetadataKeyIsTheNameTheGatewaySends pins the literal on this side of the boundary: the two
// modules each declare the constant, and no shared code catches a rename.
func TestTheMetadataKeyIsTheNameTheGatewaySends(t *testing.T) {
	if requestIDMetadataKey != "x-request-id" {
		t.Errorf("the key is %q, want the name CHK034 fixes", requestIDMetadataKey)
	}
}

// intercept runs the unary interceptor over ctx and answers success, so a test reads the line the
// transport wrote and nothing else.
func intercept(t *testing.T, logs io.Writer, ctx context.Context) {
	t.Helper()

	interceptor := NewMetrics().UnaryInterceptor(zerolog.New(logs))
	_, err := interceptor(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: "/auth.v1.AuthService/RegisterCabber"},
		func(context.Context, any) (any, error) { return &authpb.RegisterCabberResponse{}, nil },
	)
	if err != nil {
		t.Fatalf("intercepted call: %v", err)
	}
}

// incoming is the context of a call carrying values under the key of CHK034; no argument means the
// caller sent no metadata at all.
func incoming(values ...string) context.Context {
	if values == nil {
		return context.Background()
	}
	return metadata.NewIncomingContext(context.Background(), metadata.MD{
		requestIDMetadataKey: values,
	})
}

// loggedID reads the identifier of the one line the transport wrote.
func loggedID(t *testing.T, logs *bytes.Buffer) string {
	t.Helper()

	line := strings.TrimSpace(logs.String())
	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatalf("log line %q is not JSON: %v", line, err)
	}
	if event["operation"] != "RegisterCabber" {
		t.Errorf("operation = %v, want RegisterCabber", event["operation"])
	}
	id, _ := event["request_id"].(string)
	return id
}
