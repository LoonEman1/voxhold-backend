package invite

import (
	"context"
	"fmt"
	"time"
)

const directInviteLifetime = 7 * 24 * time.Hour

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{
		repository: repository,
	}
}

func (s *Service) CreateDirect(
	ctx context.Context,
	serverID int64,
	inviterUserID int64,
	input CreateDirectInput,
) (Invite, error) {
	if serverID <= 0 || inviterUserID <= 0 {
		return Invite{}, ErrForbidden
	}

	input = input.Normalize()

	if err := input.Validate(); err != nil {
		return Invite{}, err
	}

	expiresAt := time.Now().Add(directInviteLifetime).Unix()

	createdInvite, err := s.repository.CreateDirect(ctx, serverID, inviterUserID, input.Username, expiresAt)

	if err != nil {
		return Invite{}, fmt.Errorf(
			"create direct invite: %w",
			err,
		)
	}

	return createdInvite, nil
}

func (s *Service) ListIncoming(
	ctx context.Context,
	inviteeUserID int64,
) ([]IncomingInvite, error) {
	if inviteeUserID <= 0 {
		return nil, ErrForbidden
	}

	invitations, err := s.repository.ListIncoming(
		ctx,
		inviteeUserID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list incoming invitations: %w",
			err,
		)
	}

	return invitations, nil
}

func (s *Service) Accept(
	ctx context.Context,
	inviteID int64,
	inviteeUserID int64,
) error {
	if inviteID <= 0 || inviteeUserID <= 0 {
		return ErrInviteNotFound
	}

	if err := s.repository.Accept(
		ctx,
		inviteID,
		inviteeUserID,
	); err != nil {
		return fmt.Errorf(
			"accept invitation: %w",
			err,
		)
	}

	return nil
}
