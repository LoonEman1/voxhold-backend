package server

import "errors"

var (
	ErrMembershipNotFound = errors.New("server membership not found")
	ErrOwnerCannotLeave   = errors.New("instance owner cannot delete their account")
	ErrLastServerDelete   = errors.New("the instance server cannot be deleted through the API")
)
