package message

import "errors"

const (
	DefaultPageLimit = 50
	MaxPageLimit     = 100
)

var (
	ErrPageLimitInvalid = errors.New(
		"message page limit must be positive",
	)

	ErrBeforeIDInvalid = errors.New(
		"before ID must be positive",
	)
)

type ListInput struct {
	BeforeID *int64
	Limit    int
}

func (i ListInput) Normalize() ListInput {
	if i.Limit == 0 {
		i.Limit = DefaultPageLimit
	}

	if i.Limit > MaxPageLimit {
		i.Limit = MaxPageLimit
	}

	return i
}

func (i ListInput) Validate() error {
	if i.Limit <= 0 {
		return ErrPageLimitInvalid
	}

	if i.BeforeID != nil && *i.BeforeID <= 0 {
		return ErrBeforeIDInvalid
	}

	return nil
}
