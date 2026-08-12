package invite

import (
	"errors"
	"time"
)

const (
	MinLinkLifetime             = 5 * time.Minute
	MaxLinkLifetime             = 30 * 24 * time.Hour
	MaxRegistrationLinkLifetime = 24 * time.Hour
	MaxLinkUses                 = 1000
)

var (
	ErrLinkInvalid             = errors.New("invite link is invalid or expired")
	ErrLinkLifetimeInvalid     = errors.New("invite link lifetime must be between 5 minutes and 30 days")
	ErrRegistrationLinkTooLong = errors.New("invite links that allow registration cannot live longer than 24 hours")
	ErrLinkMaxUsesInvalid      = errors.New("invite link max uses must be between 1 and 1000")
	ErrRegistrationLimitNeeded = errors.New("invite links that allow registration must have a use limit")
)

type CreateLinkInput struct {
	Lifetime          time.Duration
	MaxUses           *int
	AllowRegistration bool
}

func (input CreateLinkInput) Validate() error {
	if input.Lifetime < MinLinkLifetime || input.Lifetime > MaxLinkLifetime {
		return ErrLinkLifetimeInvalid
	}

	if input.AllowRegistration && input.Lifetime > MaxRegistrationLinkLifetime {
		return ErrRegistrationLinkTooLong
	}

	if input.MaxUses != nil && (*input.MaxUses < 1 || *input.MaxUses > MaxLinkUses) {
		return ErrLinkMaxUsesInvalid
	}

	if input.AllowRegistration && input.MaxUses == nil {
		return ErrRegistrationLimitNeeded
	}

	return nil
}

type InviteLink struct {
	ID                int64
	ServerID          int64
	ServerName        string
	CreatedBy         int64
	CreatorUsername   string
	Token             string
	ExpiresAt         int64
	MaxUses           *int
	UseCount          int
	AllowRegistration bool
	CreatedAt         int64
}

type LinkPreview struct {
	ServerID          int64
	ServerName        string
	CreatorUsername   string
	ExpiresAt         int64
	MaxUses           *int
	UseCount          int
	AllowRegistration bool
}
