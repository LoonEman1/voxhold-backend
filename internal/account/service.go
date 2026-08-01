package account

import (
	"context"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

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

func (s *Service) Register(ctx context.Context, input RegisterInput) (User, error) {
	input = input.Normalize()

	if err := input.Validate(); err != nil {
		return User{}, err
	}

	passwordHash, err := bcrypt.GenerateFromPassword(
		[]byte(input.Password),
		bcrypt.DefaultCost,
	)
	if err != nil {
		return User{}, fmt.Errorf("hash password: %w", err)
	}

	user, err := s.users.Create(ctx, input.Username, string(passwordHash))
	if err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}

	return user, nil
}
