package channel

import (
	"context"
	"fmt"
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{
		repository: repository,
	}
}

func (s *Service) Create(
	ctx context.Context,
	serverID int64,
	userID int64,
	input CreateInput,
) (Channel, error) {
	if serverID <= 0 || userID <= 0 {
		return Channel{}, ErrForbidden
	}

	input = input.Normalize()

	if err := input.Validate(); err != nil {
		return Channel{}, err
	}

	createdChannel, err := s.repository.Create(
		ctx,
		serverID,
		userID,
		input.Name,
		input.Kind,
	)
	if err != nil {
		return Channel{}, fmt.Errorf(
			"create channel: %w",
			err,
		)
	}

	return createdChannel, nil
}

func (s *Service) ListByServerID(
	ctx context.Context,
	serverID int64,
	userID int64,
) ([]Channel, error) {
	if serverID <= 0 || userID <= 0 {
		return nil, ErrForbidden
	}

	channels, err := s.repository.ListByServerID(
		ctx,
		serverID,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list server channels: %w",
			err,
		)
	}

	return channels, nil
}

func (s *Service) Update(
	ctx context.Context,
	serverID int64,
	channelID int64,
	userID int64,
	input UpdateInput,
) (Channel, error) {
	if serverID <= 0 || channelID <= 0 {
		return Channel{}, ErrNotFound
	}

	if userID <= 0 {
		return Channel{}, ErrForbidden
	}

	input = input.Normalize()

	if err := input.Validate(); err != nil {
		return Channel{}, err
	}

	updatedChannel, err := s.repository.Update(
		ctx,
		serverID,
		channelID,
		userID,
		input.Name,
	)
	if err != nil {
		return Channel{}, fmt.Errorf(
			"update channel: %w",
			err,
		)
	}

	return updatedChannel, nil
}

func (s *Service) Delete(
	ctx context.Context,
	serverID int64,
	channelID int64,
	userID int64,
) error {
	if serverID <= 0 || channelID <= 0 {
		return ErrNotFound
	}

	if userID <= 0 {
		return ErrForbidden
	}

	if err := s.repository.Delete(
		ctx,
		serverID,
		channelID,
		userID,
	); err != nil {
		return fmt.Errorf(
			"delete channel: %w",
			err,
		)
	}

	return nil
}

func (s *Service) CheckAccess(
	ctx context.Context,
	serverID int64,
	channelID int64,
	userID int64,
) error {
	if serverID <= 0 || channelID <= 0 {
		return ErrNotFound
	}

	if userID <= 0 {
		return ErrForbidden
	}

	if err := s.repository.CheckAccess(
		ctx,
		serverID,
		channelID,
		userID,
	); err != nil {
		return fmt.Errorf(
			"check channel access: %w",
			err,
		)
	}

	return nil
}
