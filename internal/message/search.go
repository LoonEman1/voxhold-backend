package message

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	DefaultSearchLimit    = 25
	MaxSearchLimit        = 50
	MaxSearchQueryLength  = 200
	DefaultContextMessage = 25
	MaxContextMessage     = 50
)

var (
	ErrSearchQueryRequired = errors.New(
		"search query is required",
	)
	ErrSearchQueryTooLong = errors.New(
		"search query is too long",
	)
	ErrSearchQueryInvalid = errors.New(
		"search query must contain a letter or number",
	)
	ErrSearchLimitInvalid = errors.New(
		"search limit must be positive",
	)
	ErrContextLimitInvalid = errors.New(
		"context limits must be positive",
	)
)

type SearchInput struct {
	Query    string
	BeforeID *int64
	Limit    int
}

func (i SearchInput) Normalize() SearchInput {
	i.Query = strings.TrimSpace(i.Query)

	if i.Limit == 0 {
		i.Limit = DefaultSearchLimit
	}

	if i.Limit > MaxSearchLimit {
		i.Limit = MaxSearchLimit
	}

	return i
}

func (i SearchInput) Validate() error {
	if i.Query == "" {
		return ErrSearchQueryRequired
	}

	if utf8.RuneCountInString(i.Query) > MaxSearchQueryLength {
		return ErrSearchQueryTooLong
	}

	if !strings.ContainsFunc(i.Query, func(value rune) bool {
		return unicode.IsLetter(value) || unicode.IsNumber(value)
	}) {
		return ErrSearchQueryInvalid
	}

	if i.Limit <= 0 {
		return ErrSearchLimitInvalid
	}

	if i.BeforeID != nil && *i.BeforeID <= 0 {
		return ErrBeforeIDInvalid
	}

	return nil
}

type SearchResult struct {
	Message     Message
	ChannelName string
}

type SearchPage struct {
	Results      []SearchResult
	NextBeforeID *int64
	HasMore      bool
}

type ContextInput struct {
	Before int
	After  int
}

func (i ContextInput) Normalize() ContextInput {
	if i.Before == 0 {
		i.Before = DefaultContextMessage
	}

	if i.After == 0 {
		i.After = DefaultContextMessage
	}

	if i.Before > MaxContextMessage {
		i.Before = MaxContextMessage
	}

	if i.After > MaxContextMessage {
		i.After = MaxContextMessage
	}

	return i
}

func (i ContextInput) Validate() error {
	if i.Before <= 0 || i.After <= 0 {
		return ErrContextLimitInvalid
	}

	return nil
}

type Context struct {
	Messages        []Message
	TargetMessageID int64
	TargetIndex     int
}
