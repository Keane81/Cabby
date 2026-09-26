package service

import (
	"context"
	"time"
)

// Retention of a dead access row (R-10): a week past its absolute limit or past its revocation.
// Nothing in the storage is deleted sooner, and no account row is deleted at all — a cabber who
// stops using the app keeps the account (FR-028).
const accessRetention = 7 * 24 * time.Hour

// CleanupInterval is how often the background purge runs (R-10).
const CleanupInterval = time.Hour

// PurgeDeadAccesses deletes the access rows the retention allows to go and reports how many. The
// cutoff is read from the injected clock, so a test moves it the same way it moves an expiry
// (R-10). A storage that cannot be reached is a dependency failure, never a partial success.
func (s *Service) PurgeDeadAccesses(ctx context.Context) (int64, error) {
	before := s.now().Add(-accessRetention)
	deleted, err := s.sessions.PurgeExpired(ctx, before)
	if err != nil {
		return 0, ErrDependency
	}
	if deleted > 0 {
		// The count is the whole report: a purge of a row nobody can use any more says nothing
		// about a cabber, and no identifier of one belongs in a log line (FR-004).
		s.logger.Info().
			Str("operation", "cleanup").
			Int64("accesses_deleted", deleted).
			Msg("dead accesses purged")
	}
	return deleted, nil
}

// RunCleanup purges on every interval until ctx is done. It is a background loop, not a request
// path: an answer the storage never gives stays in the log and is retried on the next tick, because
// a service that refuses to serve accounts over a purge that failed would trade a table growing
// for an outage.
func (s *Service) RunCleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.PurgeDeadAccesses(ctx); err != nil {
				s.logger.Error().
					Str("operation", "cleanup").
					Str("error_class", "purge_failed").
					Msg("access cleanup failed")
			}
		}
	}
}
