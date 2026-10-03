// Package requestid gives one cabber request the identifier every service logs it under. The gateway
// mints it at the HTTP entry and passes it on in the metadata of the gRPC call; a service reads it back
// from the same key — the substitute for distributed tracing this system does not have. Both halves of
// the boundary live here, so the key and the shape of the value cannot drift apart.
package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"google.golang.org/grpc/metadata"
)

// MetadataKey is the gRPC metadata name the identifier travels under.
const MetadataKey = "x-request-id"

// length is 64 bits of entropy, printed as 16 hex characters: enough to tell two requests of one
// second apart, short enough to read in a log line.
const length = 8

type contextKey struct{}

// New returns a fresh identifier.
func New() string {
	raw := make([]byte, length)
	// crypto/rand.Read is documented never to fail, so an error here would mean the process has no
	// entropy at all; it would not be a reason to refuse a cabber a request.
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)
}

// Into returns a context carrying id, which is how a handler reaches the identifier the dispatcher
// minted for its request.
func Into(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// From reads the identifier of the request a context belongs to, or an empty string when the call
// was not made through the public server.
func From(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}

// Outgoing attaches the identifier the public server minted for this request to the metadata of a
// gRPC call. A call that came by another route carries none, and inventing one here would put a value
// in the log of the callee that no line of ours repeats.
func Outgoing(ctx context.Context) context.Context {
	id := From(ctx)
	if id == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, MetadataKey, id)
}

// Incoming reads the identifier of the request an incoming gRPC call belongs to. A value that is not
// the fixed shape is discarded and a fresh one drawn: this string goes into a log line, so text of a
// caller's choosing has no business being logged, and an absent identifier would leave the services
// with nothing to join their lines by.
func Incoming(ctx context.Context) string {
	if values := metadata.ValueFromIncomingContext(ctx, MetadataKey); len(values) == 1 && valid(values[0]) {
		return values[0]
	}
	return New()
}

// valid accepts 16 lowercase hex characters and nothing else.
func valid(candidate string) bool {
	if len(candidate) != length*2 {
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
