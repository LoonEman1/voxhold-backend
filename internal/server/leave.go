package server

import "errors"

var (
	ErrMembershipNotFound = errors.New("server membership not found")
	ErrOwnerCannotLeave   = errors.New("server owner cannot leave the server")
)
