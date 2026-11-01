package account

import (
	"encoding/base64"
	"errors"
	"strings"
)

var (
	ErrUsernameInvalid           = errors.New("username must contain from 3 to 32 characters")
	ErrPasswordTooShort          = errors.New("password must contain at least 8 characters")
	ErrPasswordTooLong           = errors.New("password must not exceed 72 bytes")
	ErrPasswordsDoNotMatch       = errors.New("passwords do not match")
	ErrRegistrationInviteInvalid = errors.New("a valid registration invite is required")
	ErrUsernameTaken             = errors.New("username is already taken")
)

type RegisterInput struct {
	Username        string
	Password        string
	PasswordConfirm string
	InviteToken     string
}

func (input RegisterInput) Normalize() RegisterInput {
	input.Username = strings.TrimSpace(input.Username)
	input.InviteToken = strings.TrimSpace(input.InviteToken)

	return input
}

func (input RegisterInput) Validate() error {

	if _, _, err := NormalizeUsername(input.Username); err != nil {
		return err
	}

	if len(input.Password) < 8 {
		return ErrPasswordTooShort
	}

	if len([]byte(input.Password)) > 72 {
		return ErrPasswordTooLong
	}

	if input.Password != input.PasswordConfirm {
		return ErrPasswordsDoNotMatch
	}

	decodedToken, err := base64.RawURLEncoding.DecodeString(input.InviteToken)
	if err != nil || len(decodedToken) != sessionTokenSize {
		return ErrRegistrationInviteInvalid
	}

	return nil
}
