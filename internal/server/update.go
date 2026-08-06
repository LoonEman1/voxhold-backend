package server

import "errors"

var ErrNotFound = errors.New("server not found")

type UpdateInput = CreateInput
