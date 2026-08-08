package profile

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrInvalidUserID = errors.New(
		"invalid user ID",
	)

	ErrProfileNotFound = errors.New(
		"profile not found",
	)
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{
		repository: repository,
	}
}

func (s *Service) GetByUserID(
	ctx context.Context,
	userID int64,
) (Profile, error) {
	if userID <= 0 {
		return Profile{}, ErrInvalidUserID
	}

	foundProfile, err := s.repository.GetByUserID(
		ctx,
		userID,
	)
	if err != nil {
		return Profile{}, fmt.Errorf(
			"get user profile: %w",
			err,
		)
	}

	return foundProfile, nil
}

func (s *Service) Update(
	ctx context.Context,
	userID int64,
	input UpdateInput,
) (Profile, error) {
	if userID <= 0 {
		return Profile{}, ErrInvalidUserID
	}

	input = input.Normalize()

	if err := input.Validate(); err != nil {
		return Profile{}, err
	}

	updatedProfile, err := s.repository.Update(
		ctx,
		userID,
		input.About,
		input.CountryCode,
	)
	if err != nil {
		return Profile{}, fmt.Errorf(
			"update user profile: %w",
			err,
		)
	}

	return updatedProfile, nil
}

func (s *Service) GetVisibleByUserID(
	ctx context.Context,
	requesterUserID int64,
	targetUserID int64,
) (Profile, error) {
	if requesterUserID <= 0 {
		return Profile{}, ErrInvalidUserID
	}

	if targetUserID <= 0 {
		return Profile{}, ErrProfileNotFound
	}

	foundProfile, err := s.repository.GetVisibleByUserID(
		ctx,
		requesterUserID,
		targetUserID,
	)
	if err != nil {
		return Profile{}, fmt.Errorf(
			"get visible user profile: %w",
			err,
		)
	}

	return foundProfile, nil
}
