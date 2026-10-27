package channel

import (
	"context"
	"errors"
	"slices"

	"voxhold-backend/internal/server"
)

var ErrAccessInvalid = errors.New("invalid channel access settings")

type Access struct {
	Restricted bool          `json:"restricted"`
	UserIDs    []int64       `json:"user_ids"`
	Roles      []server.Role `json:"roles"`
}

type AccessRepository interface {
	GetAccess(context.Context, int64, int64, int64) (Access, error)
	SetAccess(context.Context, int64, int64, int64, Access) (Access, error)
}

func (s *Service) GetAccess(ctx context.Context, serverID, channelID, userID int64) (Access, error) {
	return s.repository.GetAccess(ctx, serverID, channelID, userID)
}

func (s *Service) SetAccess(ctx context.Context, serverID, channelID, userID int64, input Access) (Access, error) {
	if len(input.UserIDs) > 1000 || len(input.Roles) > 3 {
		return Access{}, ErrAccessInvalid
	}
	for _, id := range input.UserIDs {
		if id <= 0 {
			return Access{}, ErrAccessInvalid
		}
	}
	for _, role := range input.Roles {
		if role != server.RoleOwner && role != server.RoleAdmin && role != server.RoleMember {
			return Access{}, ErrAccessInvalid
		}
	}
	input.UserIDs = slices.Clone(input.UserIDs)
	slices.Sort(input.UserIDs)
	input.UserIDs = slices.Compact(input.UserIDs)
	input.Roles = slices.Clone(input.Roles)
	slices.Sort(input.Roles)
	input.Roles = slices.Compact(input.Roles)
	value, err := s.repository.SetAccess(ctx, serverID, channelID, userID, input)
	if err != nil {
		return Access{}, err
	}
	if publisher, ok := s.events.(interface{ PublishChannelAccessChanged(int64, int64) }); ok {
		publisher.PublishChannelAccessChanged(serverID, channelID)
	}
	return value, nil
}
