package invite

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const directInviteLifetime = 7 * 24 * time.Hour

type Service struct {
	repository   Repository
	memberships  MembershipRegistrar
	events       EventPublisher
	memberEvents MemberEventPublisher
}

func (s *Service) CreateLink(
	ctx context.Context,
	serverID int64,
	creatorUserID int64,
	input CreateLinkInput,
) (InviteLink, error) {
	if serverID <= 0 || creatorUserID <= 0 {
		return InviteLink{}, ErrForbidden
	}

	if err := input.Validate(); err != nil {
		return InviteLink{}, err
	}

	token, tokenHash, err := generateLinkToken()
	if err != nil {
		return InviteLink{}, err
	}

	createdLink, err := s.repository.CreateLink(
		ctx,
		serverID,
		creatorUserID,
		tokenHash,
		time.Now().Add(input.Lifetime).Unix(),
		input.MaxUses,
		input.AllowRegistration,
	)
	if err != nil {
		return InviteLink{}, fmt.Errorf("create invite link: %w", err)
	}

	createdLink.Token = token

	return createdLink, nil
}

func (s *Service) ResolveLink(
	ctx context.Context,
	token string,
) (LinkPreview, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return LinkPreview{}, ErrLinkInvalid
	}

	preview, err := s.repository.ResolveLink(ctx, hashLinkToken(token))
	if err != nil {
		return LinkPreview{}, fmt.Errorf("resolve invite link: %w", err)
	}

	return preview, nil
}

func (s *Service) AcceptLink(
	ctx context.Context,
	token string,
	userID int64,
) (int64, bool, error) {
	token = strings.TrimSpace(token)
	if token == "" || userID <= 0 {
		return 0, false, ErrLinkInvalid
	}

	serverID, member, alreadyMember, err := s.repository.AcceptLink(
		ctx,
		hashLinkToken(token),
		userID,
	)
	if err != nil {
		return 0, false, fmt.Errorf("accept invite link: %w", err)
	}

	if !alreadyMember {
		s.memberEvents.PublishServerMemberJoined(serverID, member)
		s.memberships.AddUserToServer(userID, serverID)
	}

	return serverID, alreadyMember, nil
}

func NewService(
	repository Repository,
	memberships MembershipRegistrar,
	events EventPublisher,
	memberEvents MemberEventPublisher,
) *Service {
	return &Service{
		repository:   repository,
		memberships:  memberships,
		events:       events,
		memberEvents: memberEvents,
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

	s.events.PublishInvitationReceived(createdInvite)

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

	serverID, member, err := s.repository.Accept(
		ctx,
		inviteID,
		inviteeUserID,
	)
	if err != nil {
		return fmt.Errorf(
			"accept invitation: %w",
			err,
		)
	}

	s.memberEvents.PublishServerMemberJoined(
		serverID,
		member,
	)

	s.memberships.AddUserToServer(
		inviteeUserID,
		serverID,
	)

	return nil
}

func (s *Service) Decline(
	ctx context.Context,
	inviteID int64,
	inviteeUserID int64,
) error {
	if inviteID <= 0 || inviteeUserID <= 0 {
		return ErrInviteNotFound
	}

	if err := s.repository.Decline(
		ctx,
		inviteID,
		inviteeUserID,
	); err != nil {
		return fmt.Errorf(
			"decline invitation: %w",
			err,
		)
	}

	return nil
}
