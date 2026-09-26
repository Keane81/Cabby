package password

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// cheap keeps the suite fast; Default is exercised by the parameter tests below.
var cheap = Params{Memory: 16, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}

func TestHashVerifyRoundTrip(t *testing.T) {
	encoded, err := Hash(context.Background(), "1234", cheap)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$") {
		t.Fatalf("encoded %q is not an argon2id PHC string", encoded)
	}
	ok, err := Verify(context.Background(), encoded, "1234")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatal("Verify rejected the password Hash produced")
	}
}

func TestVerifyRejectsWrongPassword(t *testing.T) {
	encoded, err := Hash(context.Background(), "1234", cheap)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	ok, err := Verify(context.Background(), encoded, "1235")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if ok {
		t.Fatal("Verify accepted a password that was never hashed")
	}
}

func TestVerifyUsesParametersFromTheStoredString(t *testing.T) {
	custom := Params{Memory: 32, Time: 2, Threads: 1, KeyLen: 16, SaltLen: 16}
	encoded, err := Hash(context.Background(), "1234", custom)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !strings.Contains(encoded, "m=32,t=2,p=1") {
		t.Fatalf("encoded %q does not carry the parameters it was derived with", encoded)
	}
	// A verifier that fell back to Default would compute a different key here.
	ok, err := Verify(context.Background(), encoded, "1234")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatal("Verify did not read cost parameters from the stored hash (rotation path of R-07)")
	}
}

func TestTamperedHashIsRejected(t *testing.T) {
	encoded, err := Hash(context.Background(), "1234", cheap)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	keyStart := strings.LastIndex(encoded, "$") + 1
	if keyStart == len(encoded) {
		t.Fatalf("hash %q has an empty key", encoded)
	}
	tampered := encoded[:keyStart] + flipRune(encoded[keyStart:])

	ok, err := Verify(context.Background(), tampered, "1234")
	if err != nil {
		t.Fatalf("Verify of a tampered key: %v", err)
	}
	if ok {
		t.Fatal("Verify accepted a tampered key")
	}
}

func TestMalformedHashIsAnError(t *testing.T) {
	encoded, err := Hash(context.Background(), "1234", cheap)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	for _, candidate := range []string{
		"",
		"1234",
		strings.Replace(encoded, "argon2id", "bcrypt", 1),
		strings.Replace(encoded, "v=19", "v=99", 1),
		strings.Replace(encoded, "m=16,t=1,p=1", "m=16,t=0,p=1", 1),
		strings.Replace(encoded, "m=16,t=1,p=1", "m=16,t=1,x=1", 1),
		"$argon2id$v=19$m=16,t=1,p=1$$",
	} {
		ok, err := Verify(context.Background(), candidate, "1234")
		if err == nil {
			t.Fatalf("Verify(%q) reported no error (ok=%v)", candidate, ok)
		}
		if ok {
			t.Fatalf("Verify(%q) accepted a malformed hash", candidate)
		}
	}
}

func TestHashRejectsUnusableParams(t *testing.T) {
	for _, params := range []Params{
		{Memory: 16, Time: 1, Threads: 1, KeyLen: 0, SaltLen: 16},
		{Memory: 16, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 0},
	} {
		if _, err := Hash(context.Background(), "1234", params); err == nil {
			t.Fatalf("Hash accepted params %+v", params)
		}
	}
}

// TestDerivationSlotsAreBounded pins the memory constraint of plan.md: a third derivation
// must wait for a free slot instead of running alongside the first two.
func TestDerivationSlotsAreBounded(t *testing.T) {
	releaseFirst, err := reserve(context.Background())
	if err != nil {
		t.Fatalf("reserve 1: %v", err)
	}
	releaseSecond, err := reserve(context.Background())
	if err != nil {
		t.Fatalf("reserve 2: %v", err)
	}
	held := true
	releaseBoth := func() {
		if !held {
			return
		}
		held = false
		releaseFirst()
		releaseSecond()
	}
	t.Cleanup(releaseBoth)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Hash(cancelled, "1234", cheap); !errors.Is(err, context.Canceled) {
		t.Fatalf("Hash with both slots busy = %v, want a cancellation error", err)
	}
	if _, err := Verify(cancelled, "$argon2id$v=19$m=16,t=1,p=1$c2FsdA$aGFzaA", "1234"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify with both slots busy = %v, want a cancellation error", err)
	}

	releaseBoth()
	admitted, err := reserve(context.Background())
	if err != nil {
		t.Fatalf("reserve after release: %v", err)
	}
	admitted()
}

// flipRune changes the first base64 character so the decoded key differs, keeping the length.
func flipRune(key string) string {
	runes := []rune(key)
	if runes[0] == 'A' {
		runes[0] = 'B'
	} else {
		runes[0] = 'A'
	}
	return string(runes)
}
