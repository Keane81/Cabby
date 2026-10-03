// Package grpcobs is the observability every gRPC service of the system puts in front of its
// handlers: a panic guard, one log line per call and a hook for the metrics of the same outcome. The
// shape of the line — `operation` and `request_id` always, `error_class` only when the call did not
// succeed — is the one the gateway writes, so one search covers every service.
package grpcobs

import (
	"context"
	"time"

	"github.com/Keane81/Cabby/backend/platform/requestid"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Options configure the interceptor of one service.
type Options struct {
	// Service names the service in log messages and in the status of a panicked call.
	Service string
	Logger  zerolog.Logger
	// Method reduces a full gRPC method name to a value of a closed set. The result goes into a log
	// line and a metric label, so text of a caller's choosing must never come out of it.
	Method func(fullMethod string) string
	// Observe records one finished call; nil records nothing.
	Observe func(method string, code codes.Code, seconds float64)
}

// UnaryInterceptor chains the observability of every call: the panic guard first, so a defective
// case cannot take the process down, then the log line and the metrics of the same outcome.
func UnaryInterceptor(options Options) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (response any, err error) {
		started := time.Now()
		id := requestid.Incoming(ctx)
		defer func() {
			if panicked := recover(); panicked != nil {
				// The panic value is deliberately not logged: it can carry the request it was
				// raised on, and a request carries personal data.
				response, err = nil, status.Error(codes.Internal, options.Service+": request panicked")
			}
			method := options.Method(info.FullMethod)
			code := CodeOf(response, err)
			if options.Observe != nil {
				options.Observe(method, code, time.Since(started).Seconds())
			}
			logRequest(options, id, method, code)
		}()
		return handler(ctx, request)
	}
}

// CodeOf reports the outcome of a call. An error that carries no gRPC status, and a handler that
// answers with neither a value nor a failure, are defects of the case rather than client errors.
func CodeOf(response any, err error) codes.Code {
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

// logRequest writes one line per call. No value of the request appears in it.
func logRequest(options Options, id, method string, code codes.Code) {
	event := options.Logger.Info()
	if code != codes.OK {
		event = options.Logger.Warn()
	}
	event = event.Str("request_id", id).Str("operation", method)
	if code != codes.OK {
		event.Str("error_class", code.String()).Msg(options.Service + " request rejected")
		return
	}
	event.Msg(options.Service + " request")
}
