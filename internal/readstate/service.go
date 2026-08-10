package readstate

import (
	"context"
	"fmt"
)

type Service struct {
	repository Repository
	events     EventPublisher
}

func NewService(
	repository Repository,
	events EventPublisher,
) *Service {
	return &Service{
		repository: repository,
		events:     events,
	}
}

func (s *Service) Mark(
	ctx context.Context,
	serverID int64,
	channelID int64,
	userID int64,
	input MarkInput,
) (ChannelRead, error) {
	if serverID <= 0 || channelID <= 0 {
		return ChannelRead{}, ErrChannelNotFound
	}

	if userID <= 0 {
		return ChannelRead{}, ErrForbidden
	}

	if err := input.Validate(); err != nil {
		return ChannelRead{}, err
	}

	read, changed, err := s.repository.Mark(
		ctx,
		serverID,
		channelID,
		userID,
		input.LastReadMessageID,
	)
	if err != nil {
		return ChannelRead{}, fmt.Errorf(
			"mark channel read: %w",
			err,
		)
	}

	if changed {
		s.events.PublishChannelRead(read)
	}

	return read, nil
}

func (s *Service) ListByUserID(
	ctx context.Context,
	userID int64,
) ([]ChannelRead, error) {
	if userID <= 0 {
		return nil, ErrForbidden
	}

	reads, err := s.repository.ListByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf(
			"list user channel reads: %w",
			err,
		)
	}

	return reads, nil
}

func (s *Service) ListByChannelID(
	ctx context.Context,
	serverID int64,
	channelID int64,
	requesterUserID int64,
) ([]ChannelRead, error) {
	if serverID <= 0 || channelID <= 0 {
		return nil, ErrChannelNotFound
	}

	if requesterUserID <= 0 {
		return nil, ErrForbidden
	}

	reads, err := s.repository.ListByChannelID(
		ctx,
		serverID,
		channelID,
		requesterUserID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list channel reads: %w",
			err,
		)
	}

	return reads, nil
}
