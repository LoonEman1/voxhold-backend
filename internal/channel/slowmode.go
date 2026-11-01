package channel

import (
	"context"
	"errors"
)

const MaxSlowmodeSeconds = 21600

var ErrSlowmodeInvalid = errors.New("slowmode must be between 0 and 21600 seconds")

type SlowmodeRepository interface {
	GetSlowmode(ctx context.Context, serverID, channelID, userID int64) (int, error)
	SetSlowmode(ctx context.Context, serverID, channelID, userID int64, seconds int) error
}

func (s *Service) GetSlowmode(ctx context.Context, serverID, channelID, userID int64) (int, error) {
	repo, ok := s.repository.(SlowmodeRepository)
	if !ok {
		return 0, ErrAccessInvalid
	}
	return repo.GetSlowmode(ctx, serverID, channelID, userID)
}

func (s *Service) SetSlowmode(ctx context.Context, serverID, channelID, userID int64, seconds int) error {
	if seconds < 0 || seconds > MaxSlowmodeSeconds {
		return ErrSlowmodeInvalid
	}
	repo, ok := s.repository.(SlowmodeRepository)
	if !ok {
		return ErrAccessInvalid
	}
	return repo.SetSlowmode(ctx, serverID, channelID, userID, seconds)
}
