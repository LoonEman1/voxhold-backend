package account

import (
	"errors"
	"strings"
)

var (
	ErrLoginUsernameRequired = errors.New("username is required")
	ErrLoginPasswordRequired = errors.New("password is required")
	ErrInvalidCredentials    = errors.New("invalid username or password")
)

type LoginInput struct {
	Username string
	Password string
}

func (input LoginInput) Normalize() LoginInput {
	input.Username = strings.TrimSpace(input.Username)

	return input
}

func (input LoginInput) Validate() error {
	if input.Username == "" {
		return ErrLoginUsernameRequired
	}

	if input.Password == "" {
		return ErrLoginPasswordRequired
	}

	return nil
}

type UserInfo struct {
	ID        int64
	Username  string
	CreatedAt int64
}

type SessionInfo struct {
	Token     string
	ExpiresAt int64
}

type LoginResult struct {
	User    UserInfo
	Session SessionInfo
}
