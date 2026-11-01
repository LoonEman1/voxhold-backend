package account

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var ErrUsernameCharacters = errors.New("username may contain letters, digits, single spaces, '_', '.' and '-'")

// NormalizeUsername validates a new username and returns the display form
// (NFC, trimmed) and the case-folded lookup key that must be unique.
func NormalizeUsername(raw string) (display, key string, err error) {
	display = norm.NFC.String(strings.TrimSpace(raw))
	length := utf8.RuneCountInString(display)
	if length < 3 || length > 32 {
		return "", "", ErrUsernameInvalid
	}
	previousSpace := false
	for _, r := range display {
		switch {
		case r == ' ':
			if previousSpace {
				return "", "", ErrUsernameCharacters
			}
			previousSpace = true
			continue
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '_', r == '.', r == '-':
		default:
			return "", "", ErrUsernameCharacters
		}
		previousSpace = false
	}
	return display, UsernameKey(display), nil
}

// UsernameKey is the case-insensitive, compatibility-folded identity of a name.
func UsernameKey(display string) string {
	return strings.ToLower(norm.NFKC.String(display))
}
