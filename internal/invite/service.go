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
