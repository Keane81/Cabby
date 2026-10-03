package grpcobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Keane81/Cabby/backend/platform/requestid"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type observed struct {
	method string
	code   codes.Code
	calls  int
}

func run(
	t *testing.T,
	ctx context.Context,
	handler grpc.UnaryHandler,
) (logged map[string]any, seen observed, response any, err error) {
	t.Helper()

	var logs bytes.Buffer
	interceptor := UnaryInterceptor(Options{
		Service: "demo",
		Logger:  zerolog.New(&logs),
		Method:  func(full string) string { return full[strings.LastIndexByte(full, '/')+1:] },
		Observe: func(method string, code codes.Code, _ float64) {
			seen = observed{method: method, code: code, calls: seen.calls + 1}
		},
	})
	response, err = interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/demo.v1.Demo/Do"}, handler)
	if line := strings.TrimSpace(logs.String()); line != "" {
		if jsonErr := json.Unmarshal([]byte(line), &logged); jsonErr != nil {
			t.Fatalf("log line %q is not JSON: %v", line, jsonErr)
		}
	}
	return logged, seen, response, err
}

func incoming(id string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs(requestid.MetadataKey, id))
}

func TestSuccessIsLoggedAtInfoWithTheIdentifierOfTheCall(t *testing.T) {
	logged, seen, _, err := run(t, incoming("0123456789abcdef"),
		func(context.Context, any) (any, error) { return struct{}{}, nil })
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if logged["level"] != "info" || logged["message"] != "demo request" ||
		logged["operation"] != "Do" || logged["request_id"] != "0123456789abcdef" {
		t.Errorf("line = %v", logged)
	}
	if _, has := logged["error_class"]; has {
		t.Errorf("a successful call carries error_class: %v", logged)
	}
	if seen.calls != 1 || seen.method != "Do" || seen.code != codes.OK {
		t.Errorf("observed = %+v, want one OK call of Do", seen)
	}
}

func TestRejectionIsLoggedAtWarnWithItsClass(t *testing.T) {
	logged, seen, _, err := run(t, context.Background(),
		func(context.Context, any) (any, error) { return nil, status.Error(codes.InvalidArgument, "secret") })
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("err = %v, want the status of the handler", err)
	}
	if logged["level"] != "warn" || logged["message"] != "demo request rejected" ||
		logged["error_class"] != "InvalidArgument" {
		t.Errorf("line = %v", logged)
	}
	if strings.Contains(logged["message"].(string), "secret") {
		t.Errorf("the status message reached the log: %v", logged)
	}
	if seen.code != codes.InvalidArgument {
		t.Errorf("observed code = %v", seen.code)
	}
}

func TestPanicBecomesInternalAndLeaksNothing(t *testing.T) {
	logged, seen, response, err := run(t, context.Background(),
		func(context.Context, any) (any, error) { panic("password=hunter2") })
	if response != nil || status.Code(err) != codes.Internal || status.Convert(err).Message() != "demo: request panicked" {
		t.Fatalf("response = %v, err = %v, want INTERNAL without a cause", response, err)
	}
	if seen.code != codes.Internal || logged["error_class"] != "Internal" {
		t.Errorf("observed = %+v, line = %v", seen, logged)
	}
	for key, value := range logged {
		if text, ok := value.(string); ok && strings.Contains(text, "hunter2") {
			t.Errorf("the panic value reached the log under %q", key)
		}
	}
}

func TestCodeOf(t *testing.T) {
	for _, tc := range []struct {
		desc     string
		response any
		err      error
		want     codes.Code
	}{
		{"value", struct{}{}, nil, codes.OK},
		{"no value and no error", nil, nil, codes.Internal},
		{"status", nil, status.Error(codes.NotFound, ""), codes.NotFound},
		{"plain error", nil, errors.New("x"), codes.Internal},
	} {
		if got := CodeOf(tc.response, tc.err); got != tc.want {
			t.Errorf("%s: CodeOf = %v, want %v", tc.desc, got, tc.want)
		}
	}
}
