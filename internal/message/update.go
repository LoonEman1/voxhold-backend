package message

import (
	"strings"
	"unicode/utf8"
)

type UpdateInput struct {
	Content string
}

func (i UpdateInput) Normalize() UpdateInput {
	i.Content = strings.TrimSpace(i.Content)

	return i
}

func (i UpdateInput) Validate() error {
	if i.Content == "" {
		return ErrContentRequired
	}

	if utf8.RuneCountInString(i.Content) > MaxContentLength {
		return ErrContentTooLong
	}

	return nil
}
