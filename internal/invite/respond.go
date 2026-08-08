package invite

import "errors"

var (
	ErrInviteNotFound   = errors.New("invitation not found")
	ErrInviteNotPending = errors.New("invitation is no longer pending")
	ErrInviteExpired    = errors.New("invitation has expired")
)
