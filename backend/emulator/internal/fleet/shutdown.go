package fleet

import (
	"context"
	"sync"

	"github.com/Keane81/Cabby/backend/emulator/internal/cabber"
)

// logout revokes the sessions of the stopped cabbers with bounded concurrency and an overall
// deadline (research.md R-04). A session that misses the deadline is left to expire on its own and
// is counted as not logged out. When force is done the phase ends at once.
func (f *Fleet) logout(force context.Context, cabbers []*cabber.Cabber) {
	ctx, cancel := context.WithTimeout(force, f.p.ShutdownTimeout)
	defer cancel()

	queue := make(chan *cabber.Cabber)
	var wg sync.WaitGroup
	for w := 0; w < f.p.LogoutConcurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range queue {
				c.Logout(ctx)
			}
		}()
	}
feed:
	for i, c := range cabbers {
		if c.Token() == "" {
			continue
		}
		select {
		case queue <- c:
		case <-ctx.Done():
			// The deadline or a second signal: whoever has not been handed to a worker keeps the session.
			for _, rest := range cabbers[i:] {
				if rest.Token() != "" {
					f.p.Stats.NotLoggedOut.Add(1)
				}
			}
			break feed
		}
	}
	close(queue)
	wg.Wait()
}
