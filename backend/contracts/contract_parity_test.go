package contracts

import (
	"bytes"
	"os"
	"testing"
)

const (
	// canonicalProto is the source of truth for generated code and for every
	// service that consumes the contract.
	canonicalProto = "proto/auth/v1/auth.proto"
	// referenceProto is the specification copy the canonical file must match.
	referenceProto = "../../specs/003-cabber-auth/contracts/auth.proto"
)

// TestCanonicalProtoMatchesRepositoryReference keeps the published contract and
// its specification copy byte-identical, the same way the external REST contract
// is guarded in cabby-gateway. A drift means one copy was edited without the other.
func TestCanonicalProtoMatchesRepositoryReference(t *testing.T) {
	canonical, err := os.ReadFile(canonicalProto)
	if err != nil {
		t.Fatalf("read canonical %s: %v", canonicalProto, err)
	}
	reference, err := os.ReadFile(referenceProto)
	if err != nil {
		t.Fatalf("read reference %s: %v", referenceProto, err)
	}
	if !bytes.Equal(canonical, reference) {
		t.Fatalf("%s drifted from %s: both copies must be updated in the same change",
			canonicalProto, referenceProto)
	}
}
