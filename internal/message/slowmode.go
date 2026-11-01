package message

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// SlowmodeError reports how long the author still has to wait.
type SlowmodeError struct{ RetryAfter time.Duration }

func (e *SlowmodeError) Error() string {
	return fmt.Sprintf("slowmode: wait %d seconds before sending another message", int(e.RetryAfter.Round(time.Second)/time.Second))
}

// SlowmodeRepository is implemented by repositories that enforce per-channel
// send intervals; owners and admins are exempt.
type SlowmodeRepository interface {
	CheckSlowmode(ctx context.Context, serverID, channelID, userID int64) (time.Duration, error)
}

func (s *Service) checkSlowmode(ctx context.Context, serverID, channelID, userID int64) error {
	repo, ok := s.repository.(SlowmodeRepository)
	if !ok {
		return nil
	}
	wait, err := repo.CheckSlowmode(ctx, serverID, channelID, userID)
	if err != nil {
		return fmt.Errorf("check slowmode: %w", err)
	}
	if wait > 0 {
		return &SlowmodeError{RetryAfter: wait}
	}
	return nil
}

// IsSlowmode extracts the retry delay from an error chain.
func IsSlowmode(err error) (time.Duration, bool) {
	var slow *SlowmodeError
	if errors.As(err, &slow) {
		return slow.RetryAfter, true
	}
	return 0, false
}
