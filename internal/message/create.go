package message

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const MaxContentLength = 5120

var (
	ErrContentRequired = errors.New(
		"message content is required",
	)

	ErrContentTooLong = errors.New(
		"message content is too long",
	)

	ErrForbidden = errors.New(
		"not allowed to access messages",
	)

	ErrChannelNotFound = errors.New(
		"channel not found",
	)

	ErrTextChannelRequired = errors.New(
		"messages can only be sent to text channels",
	)

	ErrMessageNotFound = errors.New(
		"message not found",
	)

	ErrEditForbidden = errors.New(
		"only the message author can edit it",
	)

	ErrDeleteForbidden = errors.New(
		"not allowed to delete this message",
	)

	ErrPinForbidden = errors.New(
		"not allowed to manage pinned messages",
	)
)

type CreateInput struct {
	Content string
}

func (i CreateInput) Normalize() CreateInput {
	i.Content = strings.TrimSpace(i.Content)

	return i
}

func (i CreateInput) Validate() error {
	if i.Content == "" {
		return ErrContentRequired
	}

	if utf8.RuneCountInString(i.Content) > MaxContentLength {
		return ErrContentTooLong
	}

	return nil
}
