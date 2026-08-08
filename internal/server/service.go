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

func (s *Service) Delete(
	ctx context.Context,
	serverID int64,
	userID int64,
) error {
	if serverID <= 0 {
		return ErrNotFound
	}

	if err := s.repository.Delete(
		ctx,
		serverID,
		userID,
	); err != nil {
		return fmt.Errorf(
			"delete server: %w",
			err,
		)
	}

	return nil
}

func (s *Service) Leave(
	ctx context.Context,
	serverID int64,
	userID int64,
) error {
	if serverID <= 0 || userID <= 0 {
		return ErrMembershipNotFound
	}

	if err := s.repository.Leave(
		ctx,
		serverID,
		userID,
	); err != nil {
		return fmt.Errorf(
			"leave server: %w",
			err,
		)
	}

	return nil
}

func (s *Service) ListByUserID(
	ctx context.Context,
	userID int64,
) ([]JoinedServer, error) {
	joinedServers, err := s.repository.ListByUserID(
		ctx,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list user servers: %w",
			err,
		)
	}

	return joinedServers, nil
}
