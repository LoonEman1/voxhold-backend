package server

import (
	"context"
	"fmt"
)

type Service struct {
	repository    Repository
	accessRevoker AccessRevoker
	events        EventPublisher
}

func NewService(
	repository Repository,
	accessRevoker AccessRevoker,
	events EventPublisher,
) *Service {
	return &Service{
		repository:    repository,
		accessRevoker: accessRevoker,
		events:        events,
	}
}

func (s *Service) GetInstance(ctx context.Context) (Instance, error) {
	instance, err := s.repository.GetInstance(ctx)
	if err != nil {
		return Instance{}, fmt.Errorf("get Voxhold instance: %w", err)
	}

	return instance, nil
}

func (s *Service) Create(
	ctx context.Context,
	createdBy int64,
	input CreateInput,
) (Server, error) {
	if createdBy <= 0 {
		return Server{}, ErrNotFound
	}

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

	s.accessRevoker.AddUserToServer(
		createdBy,
		createdServer.ID,
	)

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

	s.accessRevoker.RevokeServer(serverID)

	return nil
}

func (s *Service) DeleteAccount(
	ctx context.Context,
	userID int64,
) error {
	if userID <= 0 {
		return ErrMembershipNotFound
	}

	serverID, err := s.repository.DeleteAccount(
		ctx,
		userID,
	)
	if err != nil {
		return fmt.Errorf(
			"delete instance account: %w",
			err,
		)
	}

	s.accessRevoker.RevokeUserFromServer(
		userID,
		serverID,
	)

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

func (s *Service) ListMembers(
	ctx context.Context,
	serverID int64,
	requesterUserID int64,
) ([]ServerMember, error) {
	if serverID <= 0 || requesterUserID <= 0 {
		return nil, ErrMembersForbidden
	}

	members, err := s.repository.ListMembers(
		ctx,
		serverID,
		requesterUserID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list server members: %w",
			err,
		)
	}

	return members, nil
}

func (s *Service) UpdateMemberRole(
	ctx context.Context,
	serverID int64,
	requesterUserID int64,
	targetUserID int64,
	input UpdateMemberRoleInput,
) (ServerMember, error) {
	if serverID <= 0 || requesterUserID <= 0 {
		return ServerMember{}, ErrManageMembersForbidden
	}

	if targetUserID <= 0 {
		return ServerMember{}, ErrMemberNotFound
	}

	if err := input.Validate(); err != nil {
		return ServerMember{}, err
	}

	member, changed, err := s.repository.UpdateMemberRole(
		ctx,
		serverID,
		requesterUserID,
		targetUserID,
		input.Role,
	)
	if err != nil {
		return ServerMember{}, fmt.Errorf(
			"update server member role: %w",
			err,
		)
	}

	if changed {
		s.events.PublishServerMemberRoleUpdated(
			serverID,
			member,
		)
	}

	return member, nil
}

func (s *Service) BanMember(
	ctx context.Context,
	serverID int64,
	requesterUserID int64,
	targetUserID int64,
) error {
	if serverID <= 0 || requesterUserID <= 0 {
		return ErrManageMembersForbidden
	}

	if targetUserID <= 0 {
		return ErrMemberNotFound
	}

	if err := s.repository.BanMember(
		ctx,
		serverID,
		requesterUserID,
		targetUserID,
	); err != nil {
		return fmt.Errorf(
			"ban instance member: %w",
			err,
		)
	}

	s.accessRevoker.RevokeUserFromServer(
		targetUserID,
		serverID,
	)

	return nil
}
