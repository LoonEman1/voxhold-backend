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
	Content          string
	ClientMessageID  string
	ReplyToMessageID *int64
}

func (i CreateInput) Normalize() CreateInput {
	i.Content = strings.TrimSpace(i.Content)

	return i
}

func (i CreateInput) Validate() error {
	if i.ReplyToMessageID != nil && *i.ReplyToMessageID <= 0 {
		return ErrReplyInvalid
	}
	if len(i.ClientMessageID) > 64 {
		return ErrClientMessageIDInvalid
	}
	for _, char := range i.ClientMessageID {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_') {
			return ErrClientMessageIDInvalid
		}
	}
	if i.Content == "" {
		return ErrContentRequired
	}

	if utf8.RuneCountInString(i.Content) > MaxContentLength {
		return ErrContentTooLong
	}

	return nil
}
