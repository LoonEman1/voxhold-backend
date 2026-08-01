package account

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var (
	ErrUsernameInvalid     = errors.New("username must contain from 3 to 32 characters")
	ErrPasswordTooShort    = errors.New("password must contain at least 8 characters")
	ErrPasswordTooLong     = errors.New("password must not exceed 72 bytes")
	ErrPasswordsDoNotMatch = errors.New("passwords do not match")
)

type RegisterInput struct {
	Username        string
	Password        string
	PasswordConfirm string
}

func (input RegisterInput) Normalize() RegisterInput {
	input.Username = strings.TrimSpace(input.Username)

	return input
}

func (input RegisterInput) Validate() error {

	usernameLength := utf8.RuneCountInString(input.Username) // длина рун
	if usernameLength < 3 || usernameLength > 32 {
		return ErrUsernameInvalid
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

	return nil
}
