package invite

import (
	"errors"
	"testing"
	"time"
)

func TestCreateLinkInputValidation(t *testing.T) {
	maxUses := 1

	tests := []struct {
		name  string
		input CreateLinkInput
		err   error
	}{
		{name: "registered users", input: CreateLinkInput{Lifetime: 30 * 24 * time.Hour}},
		{name: "registration", input: CreateLinkInput{Lifetime: 24 * time.Hour, MaxUses: &maxUses, AllowRegistration: true}},
		{name: "registration requires limit", input: CreateLinkInput{Lifetime: time.Hour, AllowRegistration: true}, err: ErrRegistrationLimitNeeded},
		{name: "registration expires within day", input: CreateLinkInput{Lifetime: 24*time.Hour + time.Second, MaxUses: &maxUses, AllowRegistration: true}, err: ErrRegistrationLinkTooLong},
		{name: "lifetime too short", input: CreateLinkInput{Lifetime: time.Minute}, err: ErrLinkLifetimeInvalid},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.input.Validate()
			if !errors.Is(err, test.err) {
				t.Fatalf("expected %v, got %v", test.err, err)
			}
		})
	}
}
