//go:build unix

package config

import "testing"

// Whether a huge need is refused depends on the system (macOS accepts almost anything as a soft
// limit), so only the portable part is tested: a modest need is always met.
func TestRaiseFileLimitAcceptsAModestNeed(t *testing.T) {
	if err := RaiseFileLimit(64); err != nil {
		t.Fatal(err)
	}
}
