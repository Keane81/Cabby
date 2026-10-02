package grpcserver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Keane81/Cabby/backend/auth/internal/token"
	"github.com/Keane81/Cabby/backend/contracts/authpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestVerifyCabberSessionNamesTheOwnerOfTheAccess is the transport half of FR-009 of spec 004: the
// caller learns who the presented access belongs to and nothing is revoked on the way.
func TestVerifyCabberSessionNamesTheOwnerOfTheAccess(t *testing.T) {
	sessions := newMemorySessions()
	server := serveWith(t, newMemoryCabbers(), sessions)
	ctx := context.Background()
	access := createSessionOverTransport(t, server, ctx)

	answer, err := server.VerifyCabberSession(ctx, &authpb.VerifyCabberSessionRequest{AccessToken: access})
	if err != nil {
		t.Fatalf("VerifyCabberSession: %v", err)
	}
	if answer.GetCabberId() == "" {
		t.Error("the answer names no owner")
	}
	stored, found, err := sessions.GetByDigest(ctx, token.Digest(access))
	if err != nil || !found || !stored.RevokedAt.IsZero() {
		t.Errorf("the check changed the session: found=%v revoked=%v err=%v", found, stored.RevokedAt, err)
	}
	server.assertCount(t, `cabby_auth_requests_total{method="VerifyCabberSession",outcome="success"} 1`)
}

// TestVerifyCabberSessionRefusesEveryDeadAccessAsTheExitDoes keeps the two operations
// indistinguishable to a caller: an unknown, a revoked and an absent access get one status in one
// text, the very one DeleteCabberSession answers with.
func TestVerifyCabberSessionRefusesEveryDeadAccessAsTheExitDoes(t *testing.T) {
	server := serveWith(t, newMemoryCabbers(), newMemorySessions())
	ctx := context.Background()
	access := createSessionOverTransport(t, server, ctx)
	if _, err := server.DeleteCabberSession(ctx, &authpb.DeleteCabberSessionRequest{AccessToken: access}); err != nil {
		t.Fatalf("DeleteCabberSession: %v", err)
	}
	_, exitErr := server.DeleteCabberSession(ctx, &authpb.DeleteCabberSessionRequest{AccessToken: access})

	for _, credential := range []string{"a session nobody issued", access, ""} {
		_, err := server.VerifyCabberSession(ctx, &authpb.VerifyCabberSessionRequest{AccessToken: credential})
		if got := status.Code(err); got != codes.Unauthenticated {
			t.Errorf("VerifyCabberSession(%q) = %v, want UNAUTHENTICATED", credential, got)
			continue
		}
		if got, want := status.Convert(err).String(), status.Convert(exitErr).String(); got != want {
			t.Errorf("VerifyCabberSession(%q) answered %q, the exit answers %q", credential, got, want)
		}
	}
}

func TestVerifyCabberSessionReportsStorageAsUnavailable(t *testing.T) {
	sessions := newMemorySessions()
	server := serveWith(t, newMemoryCabbers(), sessions)
	ctx := context.Background()
	access := createSessionOverTransport(t, server, ctx)
	sessions.getErr = errors.New("auth: select cabber_session: " + string(token.Digest(access)))

	_, err := server.VerifyCabberSession(ctx, &authpb.VerifyCabberSessionRequest{AccessToken: access})
	if got := status.Code(err); got != codes.Unavailable {
		t.Fatalf("VerifyCabberSession = %v, want UNAVAILABLE", got)
	}
	message := status.Convert(err).Message()
	if message != msgStorageGone {
		t.Errorf("status message = %q, want the fixed %q", message, msgStorageGone)
	}
	if strings.Contains(message, access) {
		t.Error("the status text repeats the access")
	}
}
