// Package mentions parses explicit user/role tokens and plain @username text.
package mentions

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var pattern = regexp.MustCompile(`<@([0-9]{1,19})>|<@&(owner|admin|member)>|@([\p{L}\p{N}_-]+)`)

type Targets struct {
	Users map[int64]bool
	Names map[string]bool
	Roles map[string]bool
}

func (t Targets) Matches(userID int64, username, role string) bool {
	if t.Users[userID] || t.Roles[role] {
		return true
	}
	return t.Names[username]
}

// Ignore escaped mentions, emails, URL paths and backtick-delimited code.
// The UI uses the same grammar; usernames with spaces use <@userID> tokens.
func Parse(content string) Targets {
	result := Targets{Users: map[int64]bool{}, Names: map[string]bool{}, Roles: map[string]bool{}}
	ignored := make([]bool, len(content))
	for i := 0; i < len(content); {
		if content[i] != '`' {
			i++
			continue
		}
		start := i
		for i < len(content) && content[i] == '`' {
			i++
		}
		delimiter := content[start:i]
		end := strings.Index(content[i:], delimiter)
		if end < 0 {
			continue
		}
		end += i + len(delimiter)
		for j := start; j < end; j++ {
			ignored[j] = true
		}
		i = end
	}
	for _, match := range pattern.FindAllStringSubmatchIndex(content, -1) {
		start, end := match[0], match[1]
		if ignored[start] {
			continue
		}
		if start > 0 {
			previous, _ := utf8.DecodeLastRuneInString(content[:start])
			if previous == '\\' || previous == '/' || previous == '.' || previous == '@' || previous == '_' || previous == '-' || unicode.IsLetter(previous) || unicode.IsNumber(previous) {
				continue
			}
		}
		if match[2] >= 0 {
			if id, err := strconv.ParseInt(content[match[2]:match[3]], 10, 64); err == nil && id > 0 {
				result.Users[id] = true
			}
		} else if match[4] >= 0 {
			result.Roles[content[match[4]:match[5]]] = true
		} else if utf8.RuneCountInString(content[start+1:end]) <= 32 {
			result.Names[content[start+1:end]] = true
		}
	}
	return result
}
