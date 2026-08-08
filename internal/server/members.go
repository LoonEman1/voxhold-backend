package server

import "errors"

var ErrMembersForbidden = errors.New(
	"not allowed to view server members",
)
