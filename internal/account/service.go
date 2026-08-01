package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionLifetime = 30 * 24 * time.Hour

type Service struct {
	users    UserRepository
	sessions SessionRepository
}

func NewService(
	users UserRepository,
	sessions SessionRepository,
) *Service {
	return &Service{
		users:    users,
		sessions: sessions,
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

	passwordHash, err := bcrypt.GenerateFromPassword(
		[]byte(input.Password),
		bcrypt.DefaultCost,
	)
	if err != nil {
		return LoginResult{}, fmt.Errorf("hash password: %w", err)
	}

	user, err := s.users.Create(ctx, input.Username, string(passwordHash))
	if err != nil {
		return LoginResult{}, fmt.Errorf("create user: %w", err)
	}

	token, expiresAt, err := s.generateSession(ctx, user)
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

func (s *Service) Login(ctx context.Context, input LoginInput) (LoginResult, error) {
	input = input.Normalize()

	if err := input.Validate(); err != nil {
		return LoginResult{}, nil
	}

	user, err := s.users.FindByUsername(ctx, input.Username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
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

	token, tokenHash, err := generateSessionToken()
	if err != nil {
		return LoginResult{}, err
	}

	expiresAt := time.Now().Add(sessionLifetime).Unix()

	err = s.sessions.Create(ctx, user.ID, tokenHash, expiresAt)
	if err != nil {
		return LoginResult{}, fmt.Errorf(
			"create session: %w",
			err,
		)
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
