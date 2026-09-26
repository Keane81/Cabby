package password

import (
	"context"
	"strings"
	"testing"
)

// TestSecretsStayOutOfStoredValues guards FR-001 and FR-004 at the storage edge: whatever
// leaves this package is a derived value, never the password, and a rejection never repeats
// the credential back.
func TestSecretsStayOutOfStoredValues(t *testing.T) {
	const plain = "s3cr3t-pass"

	encoded, err := Hash(context.Background(), plain, cheap)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if strings.Contains(encoded, plain) {
		t.Fatal("the stored hash contains the password")
	}
	if _, err := Verify(context.Background(), encoded, "wrong-"+plain); err != nil {
		t.Fatalf("Verify of a wrong password: %v", err)
	}

	saltFree := strings.ReplaceAll(encoded, "$", "")
	if strings.Contains(saltFree, strings.ReplaceAll(plain, "-", "")) {
		t.Fatal("the password survives base64 decoding of the stored value")
	}
}

func TestSamePasswordYieldsDistinctHashes(t *testing.T) {
	ctx := context.Background()
	first, err := Hash(ctx, "1234", cheap)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	second, err := Hash(ctx, "1234", cheap)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if first == second {
		t.Fatal("two hashes of the same password are equal: the salt is not random")
	}
	for _, candidate := range []string{first, second} {
		ok, err := Verify(ctx, candidate, "1234")
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if !ok {
			t.Fatalf("Verify rejected one of the two hashes of the same password: %q", candidate)
		}
	}
}

func TestErrorsCarryNoCredential(t *testing.T) {
	const plain = "1234-abcd"
	ctx := context.Background()

	_, hashErr := Hash(ctx, plain, Params{Memory: 16, Time: 1, Threads: 1, KeyLen: 0, SaltLen: 16})
	if strings.Contains(hashErr.Error(), plain) {
		t.Fatalf("Hash error repeats the password: %v", hashErr)
	}
	_, verifyErr := Verify(ctx, "not-a-phc-string", plain)
	if strings.Contains(verifyErr.Error(), plain) {
		t.Fatalf("Verify error repeats the password: %v", verifyErr)
	}
}
