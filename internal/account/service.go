package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const dummyPasswordHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

const sessionLifetime = 30 * 24 * time.Hour

var (
	ErrUnauthorized = errors.New("Unathorized")
)

type Service struct {
	users          UserRepository
	sessions       SessionRepository
	registrations  InviteRegistrationRepository
	sessionRevoker SessionRevoker
	memberships    MembershipRegistrar
	memberEvents   MemberEventPublisher
}

func NewService(
	users UserRepository,
	sessions SessionRepository,
	registrations InviteRegistrationRepository,
	sessionRevoker SessionRevoker,
	memberships MembershipRegistrar,
	memberEvents MemberEventPublisher,
) *Service {
	return &Service{
		users:          users,
		sessions:       sessions,
		registrations:  registrations,
		sessionRevoker: sessionRevoker,
		memberships:    memberships,
		memberEvents:   memberEvents,
	}
}

func (s *Service) generateSession(ctx context.Context, user UserInfo) (string, int64, error) {
	token, tokenHash, err := generateSessionToken()

	expiresAt := time.Now().Add(sessionLifetime).Unix()

	if err != nil {
		return "", -1, err
	}

	err = s.sessions.Create(ctx, user.ID, tokenHash, expiresAt)
	if err != nil {
		return "", -1, fmt.Errorf(
			"create session: %w",
			err,
		)
	}

	return token, expiresAt, nil
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (LoginResult, error) {
	input = input.Normalize()

	if err := input.Validate(); err != nil {
		return LoginResult{}, err
	}

	inviteTokenHash := hashSessionToken(input.InviteToken)
	if err := s.registrations.ValidateRegistrationInvite(
		ctx,
		inviteTokenHash,
	); err != nil {
		return LoginResult{}, fmt.Errorf("validate registration invite: %w", err)
	}

	passwordHash, err := bcrypt.GenerateFromPassword(
		[]byte(input.Password),
		bcrypt.DefaultCost,
	)
	if err != nil {
		return LoginResult{}, fmt.Errorf("hash password: %w", err)
	}

	token, tokenHash, err := generateSessionToken()
	if err != nil {
		return LoginResult{}, err
	}

	expiresAt := time.Now().Add(sessionLifetime).Unix()
	user, serverID, member, err := s.registrations.RegisterWithInvite(
		ctx,
		input.Username,
		string(passwordHash),
		inviteTokenHash,
		tokenHash,
		expiresAt,
	)
	if err != nil {
		return LoginResult{}, fmt.Errorf("register with invite: %w", err)
	}

	s.memberEvents.PublishServerMemberJoined(serverID, member)
	s.memberships.AddUserToServer(user.ID, serverID)

	return LoginResult{
		User: UserInfo{
			ID:        user.ID,
			Username:  user.Username,
			CreatedAt: user.CreatedAt,
		},
		Session: SessionInfo{
			Token:     token,
			ExpiresAt: expiresAt,
		},
	}, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrUnauthorized

	}

	tokenHash := hashSessionToken(token)

	if err := s.sessions.DeleteByTokenHash(ctx, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	s.sessionRevoker.RevokeSession(token)

	return nil
}

func (s *Service) Login(ctx context.Context, input LoginInput) (LoginResult, error) {
	input = input.Normalize()

	if err := input.Validate(); err != nil {
		return LoginResult{}, err
	}

	user, err := s.users.FindByUsername(ctx, input.Username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_ = bcrypt.CompareHashAndPassword(
				[]byte(dummyPasswordHash),
				[]byte(input.Password),
			)
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, fmt.Errorf("find user: %w", err)
	}

	err = bcrypt.CompareHashAndPassword(
		[]byte(user.PasswordHash),
		[]byte(input.Password),
	)
	if err != nil {
		return LoginResult{}, ErrInvalidCredentials
	}

	userInfo := UserInfo{
		ID:        user.ID,
		Username:  user.Username,
		CreatedAt: user.CreatedAt,
	}

	token, expiresAt, err := s.generateSession(ctx, userInfo)
	if err != nil {
		return LoginResult{}, err
	}

	return LoginResult{
		User: UserInfo{
			ID:        user.ID,
			Username:  user.Username,
			CreatedAt: user.CreatedAt,
		},
		Session: SessionInfo{
			Token:     token,
			ExpiresAt: expiresAt,
		},
	}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (int64, error) {
	token = strings.TrimSpace(token)

	if token == "" {
		return 0, ErrUnauthorized
	}

	tokenHash := hashSessionToken(token)

	userID, err := s.sessions.FindActiveUserIDByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrUnauthorized
		}

		return 0, fmt.Errorf("authenticate session: %w", err)
	}

	return userID, nil
}

func (s *Service) Refresh(
	ctx context.Context,
	currentToken string,
) (SessionInfo, error) {
	currentToken = strings.TrimSpace(currentToken)
	if currentToken == "" {
		return SessionInfo{}, ErrUnauthorized
	}

	currentTokenHash := hashSessionToken(currentToken)

	newToken, newTokenHash, err := generateSessionToken()
	if err != nil {
		return SessionInfo{}, fmt.Errorf(
			"generate refreshed session token: %w",
			err,
		)
	}

	newExpiresAt := time.Now().
		Add(sessionLifetime).
		Unix()

	err = s.sessions.Rotate(
		ctx,
		currentTokenHash,
		newTokenHash,
		newExpiresAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SessionInfo{}, ErrUnauthorized
		}

		return SessionInfo{}, fmt.Errorf(
			"refresh session: %w",
			err,
		)
	}

	s.sessionRevoker.RevokeSession(currentToken)

	return SessionInfo{
		Token:     newToken,
		ExpiresAt: newExpiresAt,
	}, nil
}
