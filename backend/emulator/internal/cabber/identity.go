// Package cabber is the life of one emulated cabber: it registers, logs in, sends the positions
// of a walk on a fixed grid of moments, logs in again when the session is lost and logs out at
// the end (data-model.md, Cabber).
package cabber

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// emailDomain is under the reserved .test TLD: the addresses exist nowhere.
const emailDomain = "emulator.cabby.test"

// Identity is what the system knows about the cabber. It is derived, never stored: the same run
// id, seed and index always give the same values, so several processes of one run agree.
type Identity struct {
	Index    int
	Name     string
	Password string
	runID    string
}

// NewIdentity derives the identity of the cabber with the given index.
func NewIdentity(runID string, seed int64, index int) Identity {
	sum := sha256.Sum256([]byte(fmt.Sprintf("cabby-emulator|%s|%d|%d", runID, seed, index)))
	return Identity{
		Index: index,
		Name:  fmt.Sprintf("emu-%d", index),
		// Twelve characters: inside the contract's 4–16.
		Password: hex.EncodeToString(sum[:])[:12],
		runID:    strings.ToLower(runID),
	}
}

// Email is the address of the account. Attempt 0 is the normal one; a 409 on registration is
// answered once with attempt 1, an address with a different suffix.
func (i Identity) Email(attempt int) string {
	suffix := ""
	if attempt > 0 {
		suffix = fmt.Sprintf("r%d", attempt)
	}
	return fmt.Sprintf("emu-%s-%d%s@%s", i.runID, i.Index, suffix, emailDomain)
}
