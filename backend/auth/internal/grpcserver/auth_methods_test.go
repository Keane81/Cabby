package grpcserver

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/auth/internal/repo"
	"github.com/Keane81/Cabby/backend/auth/internal/token"
	"github.com/Keane81/Cabby/backend/contracts/authpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	testName   = "Иван"
	testEmail  = "ivan@example.com"
	testPlain  = "1234"
	otherPlain = "5678"
	otherEmail = "other@example.com"

	// accessLimit mirrors the lifetime the service layer issues (FR-013); the transport has to
	// carry it through unchanged.
	accessLimit = 96 * time.Hour
)

func TestRegisterCabberAnswersTheAccountItCreated(t *testing.T) {
	server := serveWith(t, newMemoryCabbers(), newMemorySessions())

	answer, err := server.RegisterCabber(context.Background(), &authpb.RegisterCabberRequest{
		Name: "  " + testName + "  ", Email: "Ivan@Example.com ", Password: testPlain,
	})
	if err != nil {
		t.Fatalf("RegisterCabber: %v", err)
	}
	if answer.GetCabberId() == "" {
		t.Error("the answer carries no identifier")
	}
	if answer.GetEmail() != testEmail {
		t.Errorf("email = %q, want the canonical %q", answer.GetEmail(), testEmail)
	}
}

// TestRegisterCabberNamesTheFirstDefect is the transport half of FR-006: the field at fault
// travels in ErrorField details, in the fixed order name → email → password.
func TestRegisterCabberNamesTheFirstDefect(t *testing.T) {
	server := serveWith(t, newMemoryCabbers(), newMemorySessions())
	ctx := context.Background()

	for _, tc := range []struct {
		desc       string
		request    *authpb.RegisterCabberRequest
		wantField  string
		wantReason string
	}{
		{
			desc:       "blank name",
			request:    &authpb.RegisterCabberRequest{Name: "   ", Email: testEmail, Password: testPlain},
			wantField:  "name",
			wantReason: "empty",
		},
		{
			desc:       "name over the limit",
			request:    &authpb.RegisterCabberRequest{Name: strings.Repeat("И", 65), Email: testEmail, Password: testPlain},
			wantField:  "name",
			wantReason: "too_long",
		},
		{
			desc:       "address without a separator",
			request:    &authpb.RegisterCabberRequest{Name: testName, Email: "ivan.example.com", Password: testPlain},
			wantField:  "email",
			wantReason: "invalid_format",
		},
		{
			desc:       "password under the limit",
			request:    &authpb.RegisterCabberRequest{Name: testName, Email: testEmail, Password: "123"},
			wantField:  "password",
			wantReason: "too_short",
		},
		{
			// Three defects at once still name one of them, and it is the first in the order.
			desc:       "every field is wrong",
			request:    &authpb.RegisterCabberRequest{},
			wantField:  "name",
			wantReason: "empty",
		},
	} {
		_, err := server.RegisterCabber(ctx, tc.request)

		if got := status.Code(err); got != codes.InvalidArgument {
			t.Errorf("%s = %v, want INVALID_ARGUMENT", tc.desc, got)
			continue
		}
		field, reason := defectOf(t, err)
		if field != tc.wantField || reason != tc.wantReason {
			t.Errorf("%s: ErrorField = %s/%s, want %s/%s", tc.desc, field, reason, tc.wantField, tc.wantReason)
		}
	}
}

func TestRegisterCabberReportsTakenEmail(t *testing.T) {
	cabbers := newMemoryCabbers()
	server := serveWith(t, cabbers, newMemorySessions())
	ctx := context.Background()

	if _, err := server.RegisterCabber(ctx, &authpb.RegisterCabberRequest{
		Name: testName, Email: testEmail, Password: testPlain,
	}); err != nil {
		t.Fatalf("first RegisterCabber: %v", err)
	}
	_, err := server.RegisterCabber(ctx, &authpb.RegisterCabberRequest{
		Name: testName, Email: "IVAN@example.com", Password: otherPlain,
	})
	if got := status.Code(err); got != codes.AlreadyExists {
		t.Fatalf("second RegisterCabber = %v, want ALREADY_EXISTS", got)
	}
	// The refusal names no field and tells nothing of the account holding the address, so a
	// duplicate registration cannot be used to probe it (FR-009).
	if details := detailsOf(t, err); len(details) != 0 {
		t.Errorf("ALREADY_EXISTS carries details %v", details)
	}
	if len(cabbers.rows) != 1 {
		t.Errorf("stored accounts = %d, want one", len(cabbers.rows))
	}
}

func TestCreateCabberSessionAnswersAnAccess(t *testing.T) {
	cabbers := newMemoryCabbers()
	sessions := newMemorySessions()
	server := serveWith(t, cabbers, sessions)
	ctx := context.Background()
	if _, err := server.RegisterCabber(ctx, &authpb.RegisterCabberRequest{
		Name: testName, Email: testEmail, Password: testPlain,
	}); err != nil {
		t.Fatalf("RegisterCabber: %v", err)
	}

	before := time.Now()
	answer, err := server.CreateCabberSession(ctx, &authpb.CreateCabberSessionRequest{
		Email: testEmail, Password: testPlain,
	})
	if err != nil {
		t.Fatalf("CreateCabberSession: %v", err)
	}
	if answer.GetAccessToken() == "" {
		t.Fatal("the answer carries no access")
	}
	// FR-013: the access expires 96 hours after it was opened, and the transport passes the
	// moment through in Unix seconds — the answer is therefore exact to the second.
	issued := time.Now().Add(accessLimit)
	if drift := time.Unix(answer.GetExpiresAtUnix(), 0).Sub(before.Add(accessLimit)); drift < -time.Second || drift > time.Second {
		t.Errorf("expires_at_unix = %d, want %s plus or minus a second (%s)", answer.GetExpiresAtUnix(), before, issued)
	}
	stored := sessions.created[0]
	if stored.CabberID == "" {
		t.Error("the stored session belongs to nobody")
	}
	if token.Equal(stored.TokenHash, []byte(answer.GetAccessToken())) {
		t.Error("the access itself reached storage instead of its digest")
	}
}

// TestCreateCabberSessionRejectsBothCausesAlike is SC-006 at the transport: the two ways a sign-in
// fails give one answer, byte for byte, and that answer is not NOT_FOUND — which would turn the
// method into a tool for listing registered addresses (FR-012).
func TestCreateCabberSessionRejectsBothCausesAlike(t *testing.T) {
	cabbers := newMemoryCabbers()
	sessions := newMemorySessions()
	server := serveWith(t, cabbers, sessions)
	ctx := context.Background()
	if _, err := server.RegisterCabber(ctx, &authpb.RegisterCabberRequest{
		Name: testName, Email: testEmail, Password: testPlain,
	}); err != nil {
		t.Fatalf("RegisterCabber: %v", err)
	}

	_, unknown := server.CreateCabberSession(ctx, &authpb.CreateCabberSessionRequest{
		Email: otherEmail, Password: testPlain,
	})
	_, wrong := server.CreateCabberSession(ctx, &authpb.CreateCabberSessionRequest{
		Email: testEmail, Password: otherPlain,
	})

	for _, tc := range []struct {
		desc string
		err  error
	}{
		{"unknown address", unknown},
		{"wrong password", wrong},
	} {
		if got := status.Code(tc.err); got != codes.Unauthenticated {
			t.Errorf("%s = %v, want UNAUTHENTICATED", tc.desc, got)
		}
	}
	if status.Convert(unknown).String() != status.Convert(wrong).String() {
		t.Errorf("the two causes answer differently: %v against %v", status.Convert(unknown), status.Convert(wrong))
	}
	if len(sessions.created) != 0 {
		t.Errorf("rejected sign-ins stored %d accesses, want none", len(sessions.created))
	}
}

// TestStorageFailuresAreUnavailable keeps an outage apart from a refusal: a client must never read
// 503 as «your credentials are wrong» (edge case «Отказ хранилища»).
func TestStorageFailuresAreUnavailable(t *testing.T) {
	ctx := context.Background()
	unreachable := func(text string) error { return errors.New(text) }

	for _, tc := range []struct {
		desc    string
		cabbers *memoryCabbers
		signIn  bool
	}{
		{
			desc:    "a new account cannot be written",
			cabbers: &memoryCabbers{rows: map[string]repo.Cabber{}, createErr: unreachable("auth: insert cabber: conn lost")},
		},
		{
			desc:    "the account cannot be read",
			cabbers: &memoryCabbers{rows: map[string]repo.Cabber{}, findErr: unreachable("auth: select cabber: conn lost")},
			signIn:  true,
		},
	} {
		server := serveWith(t, tc.cabbers, newMemorySessions())

		var err error
		if tc.signIn {
			_, err = server.CreateCabberSession(ctx, &authpb.CreateCabberSessionRequest{
				Email: testEmail, Password: testPlain,
			})
		} else {
			_, err = server.RegisterCabber(ctx, &authpb.RegisterCabberRequest{
				Name: testName, Email: testEmail, Password: testPlain,
			})
		}
		if got := status.Code(err); got != codes.Unavailable {
			t.Errorf("%s = %v, want UNAVAILABLE", tc.desc, got)
		}
	}
}

// TestAccessCannotBeWrittenIsUnavailable covers the third storage failure: the credentials were
// right, and the access still could not be opened.
func TestAccessCannotBeWrittenIsUnavailable(t *testing.T) {
	cabbers := newMemoryCabbers()
	server := serveWith(t, cabbers, &memorySessions{
		stored:    map[string]repo.Session{},
		createErr: errors.New("auth: insert cabber_session: conn lost"),
	})
	ctx := context.Background()
	if _, err := server.RegisterCabber(ctx, &authpb.RegisterCabberRequest{
		Name: testName, Email: testEmail, Password: testPlain,
	}); err != nil {
		t.Fatalf("RegisterCabber: %v", err)
	}

	_, err := server.CreateCabberSession(ctx, &authpb.CreateCabberSessionRequest{
		Email: testEmail, Password: testPlain,
	})
	if got := status.Code(err); got != codes.Unavailable {
		t.Fatalf("CreateCabberSession = %v, want UNAVAILABLE", got)
	}
}

// TestNoFailureOfTheContractUsesNotFound guards the rule the proto writes down twice: NOT_FOUND
// belongs to no operation of auth.v1.
func TestNoFailureOfTheContractUsesNotFound(t *testing.T) {
	cabbers := newMemoryCabbers()
	server := serveWith(t, cabbers, newMemorySessions())
	ctx := context.Background()

	if _, err := server.RegisterCabber(ctx, &authpb.RegisterCabberRequest{
		Name: testName, Email: testEmail, Password: testPlain,
	}); err != nil {
		t.Fatalf("RegisterCabber: %v", err)
	}

	for _, tc := range []struct {
		desc string
		err  error
	}{
		{"invalid registration", func() error {
			_, err := server.RegisterCabber(ctx, &authpb.RegisterCabberRequest{Email: testEmail, Password: testPlain})
			return err
		}()},
		{"taken email", func() error {
			_, err := server.RegisterCabber(ctx, &authpb.RegisterCabberRequest{
				Name: testName, Email: "IVAN@example.com", Password: otherPlain,
			})
			return err
		}()},
		{"sign-in of an unknown address", func() error {
			_, err := server.CreateCabberSession(ctx, &authpb.CreateCabberSessionRequest{
				Email: otherEmail, Password: testPlain,
			})
			return err
		}()},
		{"sign-out with an unknown access", func() error {
			_, err := server.DeleteCabberSession(ctx, &authpb.DeleteCabberSessionRequest{AccessToken: "unknown"})
			return err
		}()},
	} {
		if got := status.Code(tc.err); got == codes.NotFound {
			t.Errorf("%s uses NOT_FOUND", tc.desc)
		}
	}
}

// signInOverTransport opens an account and one access of it through the methods under test, and
// hands back the credential the exit tests need.
func signInOverTransport(t *testing.T, server *fixture, ctx context.Context) string {
	t.Helper()
	if _, err := server.RegisterCabber(ctx, &authpb.RegisterCabberRequest{
		Name: testName, Email: testEmail, Password: testPlain,
	}); err != nil {
		t.Fatalf("RegisterCabber: %v", err)
	}
	answer, err := server.CreateCabberSession(ctx, &authpb.CreateCabberSessionRequest{
		Email: testEmail, Password: testPlain,
	})
	if err != nil {
		t.Fatalf("CreateCabberSession: %v", err)
	}
	if answer.GetAccessToken() == "" {
		t.Fatal("the sign-in answered no access")
	}
	return answer.GetAccessToken()
}

// TestDeleteCabberSessionRevokesTheAccessItIsGiven is FR-019 at the transport: the credential of the
// request is the row that dies, and the answer is the empty message the contract defines.
func TestDeleteCabberSessionRevokesTheAccessItIsGiven(t *testing.T) {
	sessions := newMemorySessions()
	server := serveWith(t, newMemoryCabbers(), sessions)
	ctx := context.Background()
	access := signInOverTransport(t, server, ctx)

	answer, err := server.DeleteCabberSession(ctx, &authpb.DeleteCabberSessionRequest{AccessToken: access})
	if err != nil {
		t.Fatalf("DeleteCabberSession: %v", err)
	}
	if got := answer.String(); got != "" {
		t.Errorf("the exit answered %q, want an empty message", got)
	}
	revoked, found, err := sessions.GetByDigest(ctx, token.Digest(access))
	if err != nil || !found {
		t.Fatalf("the revoked access is unreadable: %v, %v", found, err)
	}
	if revoked.RevokedAt.IsZero() {
		t.Error("the access survived the exit it was carried out under")
	}
	server.assertCount(t, `cabby_auth_requests_total{method="DeleteCabberSession",outcome="success"} 1`)
}

// TestDeleteCabberSessionRefusesEveryDeadAccessAlike is FR-016 and FR-021 at the transport: a
// credential nobody holds, one already revoked and an absent one all answer one status, in one text,
// and none of them is NOT_FOUND — which would report what the service knows about accesses.
func TestDeleteCabberSessionRefusesEveryDeadAccessAlike(t *testing.T) {
	server := serveWith(t, newMemoryCabbers(), newMemorySessions())
	ctx := context.Background()
	access := signInOverTransport(t, server, ctx)
	if _, err := server.DeleteCabberSession(ctx, &authpb.DeleteCabberSessionRequest{AccessToken: access}); err != nil {
		t.Fatalf("first DeleteCabberSession: %v", err)
	}

	exit := func(credential string) error {
		_, err := server.DeleteCabberSession(ctx, &authpb.DeleteCabberSessionRequest{AccessToken: credential})
		return err
	}
	refusals := []struct {
		desc string
		err  error
	}{
		{"an access nobody issued", exit("an access nobody issued")},
		{"the access of the exit just made", exit(access)},
		{"no credential at all", exit("")},
	}
	for _, refusal := range refusals {
		if got := status.Code(refusal.err); got != codes.Unauthenticated {
			t.Errorf("%s = %v, want UNAUTHENTICATED", refusal.desc, got)
			continue
		}
		if got := status.Convert(refusal.err).String(); got != status.Convert(refusals[0].err).String() {
			t.Errorf("%s answers %q, want the one refusal %q", refusal.desc, got, status.Convert(refusals[0].err))
		}
	}
	server.assertCount(t, `cabby_auth_requests_total{method="DeleteCabberSession",outcome="unauthorized"} 3`)
}

// TestDeleteCabberSessionReportsStorageAsUnavailable keeps a lost connection out of the refusal, and
// keeps the message of the database to ourselves: it can quote the digest it was given (FR-004).
func TestDeleteCabberSessionReportsStorageAsUnavailable(t *testing.T) {
	sessions := newMemorySessions()
	server := serveWith(t, newMemoryCabbers(), sessions)
	ctx := context.Background()
	access := signInOverTransport(t, server, ctx)
	sessions.revokeErr = errors.New("auth: update cabber_session: " + string(token.Digest(access)))

	_, err := server.DeleteCabberSession(ctx, &authpb.DeleteCabberSessionRequest{AccessToken: access})
	if got := status.Code(err); got != codes.Unavailable {
		t.Fatalf("DeleteCabberSession = %v, want UNAVAILABLE", got)
	}
	if got := status.Convert(err).Message(); got != msgStorageGone {
		t.Errorf("status message = %q, want the fixed %q", got, msgStorageGone)
	}
}

// defectOf reads the single ErrorField a rejection carries.
func defectOf(t *testing.T, err error) (field, reason string) {
	t.Helper()
	details := detailsOf(t, err)
	if len(details) != 1 {
		t.Fatalf("rejection carries %d details, want one ErrorField: %v", len(details), details)
	}
	defect, ok := details[0].(*authpb.ErrorField)
	if !ok {
		t.Fatalf("detail is %T, want *authpb.ErrorField", details[0])
	}
	return defect.GetField(), defect.GetReason()
}

func detailsOf(t *testing.T, err error) []any {
	t.Helper()
	withStatus, ok := status.FromError(err)
	if !ok {
		t.Fatalf("error %v is not a gRPC status", err)
	}
	return withStatus.Details()
}
