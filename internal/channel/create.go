package channel

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var (
	ErrNameRequired      = errors.New("channel name is required")
	ErrNameTooLong       = errors.New("channel name must not exceed 64 characters")
	ErrKindInvalid       = errors.New("channel kind must be text or voice")
	ErrVoiceRequired     = errors.New("voice channel is required")
	ErrForbidden         = errors.New("not allowed to create channel")
	ErrNameAlreadyExists = errors.New("channel name already exists")
)

type CreateInput struct {
	Name string
	Kind Kind
}

func (i CreateInput) Normalize() CreateInput {
	i.Name = strings.TrimSpace(i.Name)
	i.Kind = Kind(strings.ToLower(strings.TrimSpace(string(i.Kind))))

	return i
}

func (i CreateInput) Validate() error {
	if i.Name == "" {
		return ErrNameRequired
	}

	if utf8.RuneCountInString(i.Name) > 64 {
		return ErrNameTooLong
	}

	if !i.Kind.IsValid() {
		return ErrKindInvalid
	}

	return nil
}
