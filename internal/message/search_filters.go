package message

import (
	"strings"
	"time"
)

// SearchFilters are the operators extracted from a query string. Everything
// else stays in Rest and goes to the full-text index.
type SearchFilters struct {
	From   string
	In     string
	HasFile, HasImage, HasLink bool
	Before, After              *time.Time
	Rest                       string
}

// ParseSearchFilters understands from:, in:, has:file|image|link, before:YYYY-MM-DD
// and after:YYYY-MM-DD. Unknown or malformed operators remain ordinary search text.
func ParseSearchFilters(query string) SearchFilters {
	var filters SearchFilters
	var rest []string
	for _, word := range strings.Fields(query) {
		key, value, found := strings.Cut(word, ":")
		if !found || value == "" {
			rest = append(rest, word)
			continue
		}
		switch strings.ToLower(key) {
		case "from":
			filters.From = strings.TrimPrefix(value, "@")
		case "in":
			filters.In = strings.TrimPrefix(value, "#")
		case "has":
			switch strings.ToLower(value) {
			case "file":
				filters.HasFile = true
			case "image":
				filters.HasImage = true
			case "link":
				filters.HasLink = true
			default:
				rest = append(rest, word)
			}
		case "before", "after":
			day, err := time.Parse("2006-01-02", value)
			if err != nil {
				rest = append(rest, word)
				continue
			}
			if strings.ToLower(key) == "before" {
				filters.Before = &day
			} else {
				filters.After = &day
			}
		default:
			rest = append(rest, word)
		}
	}
	filters.Rest = strings.Join(rest, " ")
	return filters
}

// Any reports whether the query used at least one operator.
func (f SearchFilters) Any() bool {
	return f.From != "" || f.In != "" || f.HasFile || f.HasImage || f.HasLink || f.Before != nil || f.After != nil
}
