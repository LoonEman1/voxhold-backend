package invite

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var (
	ErrUsernameInvalid      = errors.New("username must contain from 3 to 32 characters")
	ErrCannotInviteSelf     = errors.New("cannot invite yourself")
	ErrUserNotFound         = errors.New("user not found")
	ErrAlreadyMember        = errors.New("user is already a server member")
	ErrInviteAlreadyPending = errors.New("invitation is already pending")
	ErrForbidden            = errors.New("not allowed to invite users")
)

type CreateDirectInput struct {
	Username string
}

func (i CreateDirectInput) Normalize() CreateDirectInput {
	i.Username = strings.TrimSpace(i.Username)

	return i
}

func (i CreateDirectInput) Validate() error {
	usernameLength := utf8.RuneCountInString(i.Username)

	if usernameLength < 3 || usernameLength > 32 {
		return ErrUsernameInvalid
	}

	return nil
}
