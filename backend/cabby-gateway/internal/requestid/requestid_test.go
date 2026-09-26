package requestid

import (
	"context"
	"regexp"
	"testing"
)

var shape = regexp.MustCompile(`^[0-9a-f]{16}$`)

// TestIDsAreOfTheShapeBothModulesAgreeOn guards the value auth validates: a shorter or longer id,
// or one drawn by a different generator, would be discarded at the other end of the boundary.
func TestIDsAreOfTheShapeBothModulesAgreeOn(t *testing.T) {
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

// TestMetadataKeyNameIsTheOneAuthReads pins the literal: the two modules declare the constant on
// their own side of the boundary, so nothing but a test catches one of them renaming it.
func TestMetadataKeyNameIsTheOneAuthReads(t *testing.T) {
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
