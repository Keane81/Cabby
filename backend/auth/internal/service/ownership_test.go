package service

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The second account of these tests. Its fields differ from the first in every value a refusal
// could repeat, so a leak of either account shows up in the assertions.
const (
	otherName  = "Пётр"
	otherEmail = "pyotr@example.com"
	otherPlain = "abcd"
)

// cabber is one account with one live access, standing on storage of its own. Two of them model the
// two subjects of an ownership question: an operation of cabber B is served by the storage that
// holds B, and a credential from the other side of that line is what the tests refuse.
type cabber struct {
	service *Service
	id      string
	access  string

	name  string
	email string
	plain string
}

func registerWithSession(t *testing.T, name, email, plain string) cabber {
	t.Helper()
	ctx := context.Background()
	cabbers, sessions := newFakeCabbers(), newFakeSessions()
	service := newCaseService(cabbers, sessions)

	created, err := service.Register(ctx, name, email, plain)
	if err != nil {
		t.Fatalf("Register %s: %v", email, err)
	}
	issued, err := service.CreateSession(ctx, email, plain)
	if err != nil {
		t.Fatalf("CreateSession %s: %v", email, err)
	}
	return cabber{service: service, id: created.ID, access: issued.Token, name: name, email: email, plain: plain}
}

// TestVerifyAnswersWithItsOwnOwner is FR-015 read the other way: a session names exactly one
// account, and no request can make it name another. Verify takes a credential and nothing
// else, so the caller has no argument through which to put a subject of its own in (consequence
// R-03): the URI of an operation never decides who acts.
func TestVerifyAnswersWithItsOwnOwner(t *testing.T) {
	first, second := registerWithSession(t, testName, testEmail, testPlain), registerWithSession(t, otherName, otherEmail, otherPlain)
	ctx := context.Background()

	if owner, err := first.service.Verify(ctx, first.access); err != nil || owner != first.id {
		t.Errorf("own access = %q, %v, want %q and no error", owner, err, first.id)
	}
	if owner, err := second.service.Verify(ctx, second.access); err != nil || owner != second.id {
		t.Errorf("own access = %q, %v, want %q and no error", owner, err, second.id)
	}
}

// TestAccessOfOneCabberDoesNotReachTheOther is FR-018: the live access of one account, read by the
// operation of another, is refused in the very same form as a session nobody ever issued. The other
// access stays valid where it belongs, so the refusal cannot have reported the account behind it.
func TestAccessOfOneCabberDoesNotReachTheOther(t *testing.T) {
	first, second := registerWithSession(t, testName, testEmail, testPlain), registerWithSession(t, otherName, otherEmail, otherPlain)
	ctx := context.Background()

	owner, foreignErr := first.service.Verify(ctx, second.access)
	if !errors.Is(foreignErr, ErrInvalidSession) {
		t.Fatalf("a foreign access = %v, want ErrInvalidSession", foreignErr)
	}
	if owner != "" {
		t.Errorf("a refused access named an owner: %q", owner)
	}

	_, unknownErr := first.service.Verify(ctx, "a session nobody issued")
	if !errors.Is(unknownErr, ErrInvalidSession) {
		t.Fatalf("an unknown access = %v, want ErrInvalidSession", unknownErr)
	}
	// One error value for both, not two that merely read alike: the transport can only report the
	// one category, and that category cannot be a census of accounts (SC-003).
	if foreignErr != unknownErr {
		t.Errorf("the two refusals differ: %v against %v", foreignErr, unknownErr)
	}

	if _, err := second.service.Verify(ctx, second.access); err != nil {
		t.Fatalf("the other access stopped working on its own side: %v", err)
	}
	if owner, err := first.service.Verify(ctx, first.access); err != nil || owner != first.id {
		t.Fatalf("the refused attempt changed the first access: %q, %v", owner, err)
	}
}

// TestVerifyRefusalNamesNoAccount holds FR-004 and FR-018 on the returned failure: neither
// account appears in it, nor the credential that was refused.
func TestVerifyRefusalNamesNoAccount(t *testing.T) {
	first, second := registerWithSession(t, testName, testEmail, testPlain), registerWithSession(t, otherName, otherEmail, otherPlain)

	_, err := first.service.Verify(context.Background(), second.access)
	if err == nil {
		t.Fatal("a session of another account was accepted")
	}
	for _, value := range []string{
		first.id, second.id, first.name, second.name, first.email, second.email,
		first.plain, second.plain, first.access, second.access,
	} {
		if strings.Contains(err.Error(), value) {
			t.Errorf("the refusal repeats %q: %v", value, err)
		}
	}
}
