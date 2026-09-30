package service

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/auth/internal/password"
	"github.com/rs/zerolog"
)

// timingSamples is the number of measurements per answer. Each one costs a full Argon2id
// derivation, so the count stays low; a median of them is still stable enough to compare two paths.
const timingSamples = 9

// TestCreateSessionAnswerTimeDoesNotRevealAnAccount is SC-006 on the mechanism behind it: a session creation for an
// address nobody owns runs a derivation against decoyHash, so both refusals are one measurement of
// one derivation. The bound is a factor rather than a duration, because the assertion is about the
// two medians matching; the lower bound proves the production cost was really paid, which is the
// only reason they match at all.
func TestCreateSessionAnswerTimeDoesNotRevealAnAccount(t *testing.T) {
	ctx := context.Background()
	cabbers, sessions := newFakeCabbers(), newFakeSessions()
	service := New(cabbers, sessions, password.Default, zerolog.Nop(), time.Now)
	if _, err := service.Register(ctx, testName, testEmail, testPlain); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// The two paths alternate, so a machine that slows down partway through the test slows down
	// both medians.
	var notFound, wrongPassword []time.Duration
	for range timingSamples {
		notFound = append(notFound, refuseIn(ctx, t, service, "nobody@example.com", testPlain))
		wrongPassword = append(wrongPassword, refuseIn(ctx, t, service, testEmail, decoyPlain))
	}

	slow, fast := median(notFound), median(wrongPassword)
	if slow < fast {
		slow, fast = fast, slow
	}
	if fast < 5*time.Millisecond {
		t.Fatalf("a derivation answered in %s, so the comparison measures nothing but map lookups", fast)
	}
	if ratio := float64(slow) / float64(fast); ratio > 1.5 {
		t.Errorf("the two refusals differ by %gx: %s against %s, want within 1.5x", ratio, slow, fast)
	}
}

// refuseIn times a session creation that has to be refused, and checks it was refused for the reason the
// test intended: measuring the duration of an accepted session creation would prove nothing.
func refuseIn(ctx context.Context, t *testing.T, service *Service, email, plain string) time.Duration {
	t.Helper()
	started := time.Now()
	_, err := service.CreateSession(ctx, email, plain)
	elapsed := time.Since(started)

	if !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("CreateSession(%q) = %v, want ErrInvalidSession", email, err)
	}
	return elapsed
}

func median(durations []time.Duration) time.Duration {
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2]
}
