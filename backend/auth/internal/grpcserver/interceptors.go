package grpcserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// UnaryInterceptor chains the observability of every call: the panic guard first, so a defective
// case cannot take the process down, then the log line and the metrics of the same outcome.
func (m *Metrics) UnaryInterceptor(logger zerolog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (response any, err error) {
		started := time.Now()
		requestID := requestIDOf(ctx)
		defer func() {
			if panicked := recover(); panicked != nil {
				// The panic value is deliberately not logged: it can carry the request it
				// was raised on, and a request carries a password and an address (FR-004).
				response, err = nil, status.Error(codes.Internal, "auth: request panicked")
			}
			m.observeCall(logger, requestID, info.FullMethod, response, err, time.Since(started))
		}()
		return handler(ctx, request)
	}
}

// requestIDMetadataKey names the metadata the gateway passes its request identifier in. The two
// modules declare the same string independently — contracts carries the messages, not the transport
// metadata (CHK034).
const requestIDMetadataKey = "x-request-id"

// requestIDLength is the hex form of the 64 bits the gateway draws.
const requestIDLength = 16

// requestIDOf reads the identifier of the request the call belongs to. A value that is not the
// fixed shape is discarded and a fresh one drawn: this string goes into a log line, so text of a
// caller's choosing has no business being logged, and an absent identifier would leave the two
// services with nothing to join their lines by.
func requestIDOf(ctx context.Context) string {
	if values := metadata.ValueFromIncomingContext(ctx, requestIDMetadataKey); len(values) == 1 && isRequestID(values[0]) {
		return values[0]
	}
	raw := make([]byte, requestIDLength/2)
	// crypto/rand.Read is documented never to fail, so an error here would mean the process has no
	// entropy at all; it would not be a reason to refuse a cabber a request.
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)
}

// isRequestID accepts 16 lowercase hex characters and nothing else.
func isRequestID(candidate string) bool {
	if len(candidate) != requestIDLength {
		return false
	}
	for _, char := range candidate {
		switch {
		case '0' <= char && char <= '9', 'a' <= char && char <= 'f':
		default:
			return false
		}
	}
	return true
}

func (m *Metrics) observeCall(
	logger zerolog.Logger,
	requestID, fullMethod string,
	response any,
	err error,
	elapsed time.Duration,
) {
	method := methodOf(fullMethod)
	code := codeOf(response, err)
	m.observeRequest(method, code, elapsed.Seconds())
	logRequest(logger, requestID, method, code)
}

// codeOf reports the outcome of a call. An error that carries no gRPC status, and a handler that
// answers with neither a value nor a failure, are defects of the case rather than client errors.
func codeOf(response any, err error) codes.Code {
	if err != nil {
		if code := status.Code(err); code != codes.Unknown {
			return code
		}
		return codes.Internal
	}
	if response == nil {
		return codes.Internal
	}
	return codes.OK
}

// logRequest writes one line per call in the field shape of the gateway: `operation` and
// `request_id` always, `error_class` only when the call did not succeed. No value of the request
// appears in it.
func logRequest(logger zerolog.Logger, requestID, method string, code codes.Code) {
	event := logger.Info()
	if code != codes.OK {
		event = logger.Warn()
	}
	event = event.Str("request_id", requestID).Str("operation", method)
	if code != codes.OK {
		event.Str("error_class", code.String()).Msg("auth request rejected")
		return
	}
	event.Msg("auth request")
}
