package channel

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrNotFound = errors.New("channel not found")

type UpdateInput struct {
	Name string
}

func (i UpdateInput) Normalize() UpdateInput {
	i.Name = strings.TrimSpace(i.Name)

	return i
}

func (i UpdateInput) Validate() error {
	if i.Name == "" {
		return ErrNameRequired
	}

	if utf8.RuneCountInString(i.Name) > 64 {
		return ErrNameTooLong
	}

	return nil
}
