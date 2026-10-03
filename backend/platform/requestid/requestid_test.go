package requestid

import (
	"context"
	"regexp"
	"testing"

	"google.golang.org/grpc/metadata"
)

var shape = regexp.MustCompile(`^[0-9a-f]{16}$`)

// TestIDsAreOfTheShapeTheReceiverAccepts guards the value a service validates: a shorter or longer id,
// or one drawn by a different generator, would be discarded at the other end of the boundary.
func TestIDsAreOfTheShapeTheReceiverAccepts(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := New()
		if !shape.MatchString(id) {
			t.Fatalf("New = %q, want 16 hex characters", id)
		}
		if seen[id] {
			t.Fatalf("New returned %q twice", id)
		}
		seen[id] = true
	}
}

// TestMetadataKeyNameIsTheWireName pins the literal: a rename would silently split the logs of the
// services, and the key is the one name a client outside this repository could also rely on.
func TestMetadataKeyNameIsTheWireName(t *testing.T) {
	if MetadataKey != "x-request-id" {
		t.Errorf("MetadataKey = %q, want the name CHK034 fixes", MetadataKey)
	}
}

func TestIdentifierSurvivesTheContext(t *testing.T) {
	if got := From(context.Background()); got != "" {
		t.Errorf("From of a bare context = %q, want no identifier", got)
	}
	id := New()
	if got := From(Into(context.Background(), id)); got != id {
		t.Errorf("From = %q, want the identifier it was given", got)
	}
}

func TestOutgoingCarriesTheIdentifierOfTheContext(t *testing.T) {
	bare := metadata.ValueFromIncomingContext(context.Background(), MetadataKey)
	if len(bare) != 0 {
		t.Fatalf("bare context carries %v", bare)
	}
	if got, _ := metadata.FromOutgoingContext(Outgoing(context.Background())); len(got.Get(MetadataKey)) != 0 {
		t.Errorf("a context without an identifier got one on the wire: %v", got)
	}

	ctx := Outgoing(Into(context.Background(), "0123456789abcdef"))
	md, _ := metadata.FromOutgoingContext(ctx)
	if got := md.Get(MetadataKey); len(got) != 1 || got[0] != "0123456789abcdef" {
		t.Errorf("outgoing metadata = %v, want the identifier of the request", got)
	}
}

// TestIncomingKeepsAValidIdentifierAndReplacesTheRest guards the log line against its input: only the
// fixed shape is repeated, and a call that brought nothing usable still gets an identifier.
func TestIncomingKeepsAValidIdentifierAndReplacesTheRest(t *testing.T) {
	ctx := func(values ...string) context.Context {
		if values == nil {
			return context.Background()
		}
		return metadata.NewIncomingContext(context.Background(), metadata.MD{MetadataKey: values})
	}
	if got := Incoming(ctx("0123456789abcdef")); got != "0123456789abcdef" {
		t.Errorf("Incoming = %q, want the identifier of the call", got)
	}
	for desc, values := range map[string][]string{
		"no metadata":        nil,
		"empty value":        {""},
		"one character less": {"0123456789abcde"},
		"one character more": {"0123456789abcdef0"},
		"not hexadecimal":    {"0123456789abcdeg"},
		"uppercase":          {"0123456789ABCDEF"},
		"two values":         {"0123456789abcdef", "0123456789abcdef"},
		"text of a caller":   {"auth request\n\"level\":\"info\""},
	} {
		got := Incoming(ctx(values...))
		if !shape.MatchString(got) {
			t.Errorf("%s: Incoming = %q, want 16 hex characters of our own", desc, got)
		}
		for _, value := range values {
			if got == value {
				t.Errorf("%s: the identifier of the caller was repeated: %q", desc, got)
			}
		}
	}
}
