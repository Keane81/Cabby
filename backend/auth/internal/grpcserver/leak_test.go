package grpcserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/Keane81/Cabby/backend/contracts/authpb"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// The values one run of the transport is made of. They are long and distinct on purpose: a short
// value would be a substring of a timestamp or of a digest and the guard would report a leak where
// nothing leaked.
const (
	leakName         = "Leak-check Cabber"
	leakEmail        = "leakcheck@example.test"
	leakPlain        = "leak-plain-pass"
	leakWrong        = "leak-wrong-pass"
	leakOverLimit    = "a-password-over-the-sixteen-character-limit"
	leakRequestIDKey = "0123456789abcdef"
)

// TestNoSinkOfAuthCarriesAValueOfACall is SC-002, FR-004 and FR-025 on the internal interface: after
// the three operations have answered success and refusal, what the service writes down — its log and
// its metrics — and what a refusal says, name a request by its method and its outcome and repeat
// nothing of it: no address, no name, no password, no access, and neither the digest of an access nor
// a derived hash.
func TestNoSinkOfAuthCarriesAValueOfACall(t *testing.T) {
	var logs bytes.Buffer
	cabbers := newMemoryCabbers()
	server := servingLogged(t, &logs, cabbers, newMemorySessions())
	ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{
		requestIDMetadataKey: []string{leakRequestIDKey},
	})

	var refusals []string
	refused := func(desc string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: the call succeeded, want a refusal", desc)
		}
		refusals = append(refusals, status.Convert(err).String())
	}

	_, err := server.RegisterCabber(ctx, &authpb.RegisterCabberRequest{
		Name: leakName, Email: leakEmail, Password: leakOverLimit,
	})
	refused("a password over the limit", err)

	_, err = server.RegisterCabber(ctx, &authpb.RegisterCabberRequest{
		Name: "   ", Email: leakEmail, Password: leakPlain,
	})
	refused("a blank name", err)

	// The account is created from a request whose values the transport must now forget.
	created, err := server.RegisterCabber(ctx, &authpb.RegisterCabberRequest{
		Name: "  " + leakName + "  ", Email: "LEAKCHECK@Example.test", Password: leakPlain,
	})
	if err != nil {
		t.Fatalf("RegisterCabber: %v", err)
	}
	if created.GetEmail() != leakEmail {
		t.Fatalf("email = %q, want the canonical %q", created.GetEmail(), leakEmail)
	}

	_, err = server.RegisterCabber(ctx, &authpb.RegisterCabberRequest{
		Name: leakName, Email: leakEmail, Password: leakPlain,
	})
	refused("an address already taken", err)

	_, err = server.CreateCabberSession(ctx, &authpb.CreateCabberSessionRequest{
		Email: leakEmail, Password: leakWrong,
	})
	refused("wrong credentials", err)

	opened, err := server.CreateCabberSession(ctx, &authpb.CreateCabberSessionRequest{
		Email: leakEmail, Password: leakPlain,
	})
	if err != nil {
		t.Fatalf("CreateCabberSession: %v", err)
	}
	if opened.GetAccessToken() == "" {
		t.Fatal("the sign-in answered no access")
	}
	_, err = server.DeleteCabberSession(ctx, &authpb.DeleteCabberSessionRequest{
		AccessToken: opened.GetAccessToken(),
	})
	if err != nil {
		t.Fatalf("DeleteCabberSession: %v", err)
	}
	_, err = server.DeleteCabberSession(ctx, &authpb.DeleteCabberSessionRequest{
		AccessToken: opened.GetAccessToken(),
	})
	refused("an access already revoked", err)

	if len(refusals) != 5 {
		t.Fatalf("the run collected %d refusals, want 5", len(refusals))
	}

	// What storage holds is the far end of the same rule: a dump of the tables reaches no log line,
	// and the shape of a PHC string is what a reader of a leak would look for first.
	hash := cabbers.rows[leakEmail].PasswordHash
	digest := sha256.Sum256([]byte(opened.GetAccessToken()))
	segments := strings.Split(hash, "$")
	values := []string{
		leakName, leakEmail, "leakcheck", leakPlain, leakWrong, leakOverLimit,
		opened.GetAccessToken(),
		fmt.Sprintf("%x", digest),
		base64.RawURLEncoding.EncodeToString(digest[:]),
		hash, segments[len(segments)-2], segments[len(segments)-1],
	}

	sinks := []struct{ name, text string }{
		{"the log", logs.String()},
		{"the metrics", server.exported(t)},
	}
	for index, refusal := range refusals {
		sinks = append(sinks, struct{ name, text string }{fmt.Sprintf("refusal %d", index), refusal})
	}
	for _, sink := range sinks {
		for _, value := range values {
			if strings.Contains(sink.text, value) {
				t.Errorf("%s repeats a value of a call: %s", sink.name, sink.text)
			}
		}
	}

	// The names of the values a request carries belong to the request and, for a rejection, to the
	// contract that names the field at fault. A line about a request and a metric about a service have
	// no reason to carry one (FR-004). The label of a repository query is excluded: it is named for
	// the statement it counts, which is a name of our own code and not of a field of a call.
	for _, sink := range sinks[:2] {
		for _, field := range []string{"password", "access_token", "token_hash", "argon2"} {
			if strings.Contains(sink.text, field) {
				t.Errorf("%s names the field %q: %s", sink.name, field, sink.text)
			}
		}
	}
}

// TestRefusalOfAPasswordDefectNamesTheFieldAndNothingElse is the one answer of the transport that
// points at a part of the request: FR-006 promises the field, and nothing of its value.
func TestRefusalOfAPasswordDefectNamesTheFieldAndNothingElse(t *testing.T) {
	server := serveWith(t, newMemoryCabbers(), newMemorySessions())

	_, err := server.RegisterCabber(context.Background(), &authpb.RegisterCabberRequest{
		Name: leakName, Email: leakEmail, Password: leakOverLimit,
	})
	withStatus, ok := status.FromError(err)
	if !ok {
		t.Fatalf("error %v is not a gRPC status", err)
	}
	if withStatus.Message() != msgInvalidField {
		t.Errorf("message = %q, want the fixed text %q", withStatus.Message(), msgInvalidField)
	}
	if len(withStatus.Details()) != 1 {
		t.Fatalf("the refusal carries %d details, want one", len(withStatus.Details()))
	}
	defect, ok := withStatus.Details()[0].(*authpb.ErrorField)
	if !ok || defect.GetField() != "password" || defect.GetReason() != "too_long" {
		t.Errorf("detail = %v, want password/too_long", withStatus.Details()[0])
	}
	for _, value := range []string{leakOverLimit, leakName, leakEmail} {
		if strings.Contains(withStatus.String(), value) {
			t.Errorf("the refusal repeats a value of the request: %s", withStatus.String())
		}
	}
}
