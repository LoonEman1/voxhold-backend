package server

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var (
	ErrNameRequired  = errors.New("server name is required")
	ErrNameTooLong   = errors.New("server name must not exceed 64 characters")
	ErrAlreadyExists = errors.New("server already exists")
)

type CreateInput struct {
	Name string
}

func (i CreateInput) Normalize() CreateInput {
	i.Name = strings.TrimSpace(i.Name)

	return i
}

func (i CreateInput) Validate() error {
	if i.Name == "" {
		return ErrNameRequired
	}

	if utf8.RuneCountInString(i.Name) > 64 {
		return ErrNameTooLong
	}

	return nil
}
