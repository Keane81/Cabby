// Package requestid gives one cabber request the identifier both services log it under. The gateway
// mints it at the HTTP entry, passes it to auth in the metadata of the gRPC call, and auth reads it
// back from the same key — the substitute for distributed tracing this system does not have
// (research R-04, checklists/naming-and-stack.md CHK034).
package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// MetadataKey is the gRPC metadata name the identifier travels under. The auth service declares the
// same name in its own module: the two constants are the contract of the pair, and nothing else is
// shared between them.
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
