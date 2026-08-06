package server

import (
	"context"
	"fmt"
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(
	ctx context.Context,
	createdBy int64,
	input CreateInput,
) (Server, error) {
	input = input.Normalize()

	if err := input.Validate(); err != nil {
		return Server{}, err
	}

	createdServer, err := s.repository.Create(
		ctx,
		input.Name,
		createdBy,
	)
	if err != nil {
		return Server{}, fmt.Errorf(
			"create server: %w",
			err,
		)
	}

	return createdServer, nil
}

func (s *Service) Update(
	ctx context.Context,
	serverID int64,
	userID int64,
	input UpdateInput,
) (Server, error) {
	if serverID <= 0 {
		return Server{}, ErrNotFound
	}

	input = input.Normalize()

	if err := input.Validate(); err != nil {
		return Server{}, err
	}

	updatedServer, err := s.repository.Update(
		ctx,
		serverID,
		userID,
		input.Name,
	)
	if err != nil {
		return Server{}, fmt.Errorf(
			"update server: %w",
			err,
		)
	}

	return updatedServer, nil
}
