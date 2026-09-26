package token

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestNewProducesOpaqueAccess(t *testing.T) {
	first, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	second, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if first == second {
		t.Fatal("two calls returned the same access")
	}
	if strings.ContainsAny(first, "=+/") {
		t.Fatalf("token %q is not base64url without padding", first)
	}
	raw, err := base64.RawURLEncoding.DecodeString(first)
	if err != nil {
		t.Fatalf("token %q is not base64url: %v", first, err)
	}
	if len(raw) != 32 {
		t.Fatalf("token entropy = %d bytes, want 32", len(raw))
	}
}

func TestDigestIsStableAndNotTheAccess(t *testing.T) {
	presented, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	digest := Digest(presented)
	if len(digest) != 32 {
		t.Fatalf("digest length = %d, want 32", len(digest))
	}
	if !Equal(digest, Digest(presented)) {
		t.Fatal("digest of the same access differs between calls")
	}
	if Equal(digest, []byte(presented)) {
		t.Fatal("digest equals the presented access")
	}
	other, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if Equal(digest, Digest(other)) {
		t.Fatal("two different accesses share a digest")
	}
}

func TestEqualComparesDigestsOnly(t *testing.T) {
	if !Equal([]byte("hash"), []byte("hash")) {
		t.Fatal("Equal rejected identical digests")
	}
	for _, pair := range [][2][]byte{
		{[]byte("hash"), []byte("hask")},
		{[]byte("hash"), []byte("hash longer")},
		{nil, []byte("hash")},
	} {
		if Equal(pair[0], pair[1]) {
			t.Fatalf("Equal(%q, %q) = true", pair[0], pair[1])
		}
	}
}
