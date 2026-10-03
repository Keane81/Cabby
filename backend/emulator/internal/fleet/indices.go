// Package fleet runs the whole park: it spreads the logins over the ramp-up, gives every cabber
// its own phase on the send grid, keeps registration and login within what the auth service can
// hash, and stops the park cleanly (research.md R-03, R-04, R-09).
package fleet

import (
	"math/rand/v2"
	"time"
)

// Indices are the cabbers this process owns: j, j+k, j+2k, … below n (research.md R-09). All the
// processes of one run together cover every index exactly once.
func Indices(cabbers, instances, instance int) []int {
	owned := make([]int, 0, cabbers/instances+1)
	for i := instance; i < cabbers; i += instances {
		owned = append(owned, i)
	}
	return owned
}

// StartAt is the moment cabber i may begin to register: the logins are spread evenly over the
// ramp-up, and the schedule is global by index, so it does not depend on how many processes share
// the park.
func StartAt(runStart time.Time, index, cabbers int, rampUp time.Duration) time.Time {
	return runStart.Add(time.Duration(int64(rampUp) * int64(index) / int64(cabbers)))
}

// SendPhase is the offset of the first send of cabber i after its login, uniform in [0, interval): it
// keeps 50 000 cabbers from sending in the same millisecond. It is deterministic by seed and index.
func SendPhase(seed int64, index int, interval time.Duration) time.Duration {
	rng := rand.New(rand.NewPCG(uint64(seed)^0x9e3779b97f4a7c15, uint64(index)+1))
	return time.Duration(rng.Float64() * float64(interval))
}
