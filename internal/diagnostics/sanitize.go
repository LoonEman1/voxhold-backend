package diagnostics

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

const redactedValue = "[redacted]"

func sanitizeDetailsJSON(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "{}", nil
	}

	var details map[string]any
	if err := json.Unmarshal([]byte(value), &details); err != nil {
		return "", err
	}
	cleaned := sanitizeValue(details, 0)
	encoded, err := json.Marshal(cleaned)
	if err != nil {
		return "", err
	}
	if len(encoded) > MaxDetailsBytes {
		return `{"truncated":true}`, nil
	}
	return string(encoded), nil
}

func sanitizeValue(value any, depth int) any {
	if depth >= 4 {
		return "[max-depth]"
	}

	switch current := value.(type) {
	case map[string]any:
		result := make(map[string]any, min(len(current), 32))
		count := 0
		for key, child := range current {
			if count >= 32 {
				result["truncated"] = true
				break
			}
			if sensitiveKey(key) {
				result[key] = redactedValue
			} else {
				result[key] = sanitizeValue(child, depth+1)
			}
			count++
		}
		return result

	case []any:
		limit := min(len(current), 20)
		result := make([]any, 0, limit)
		for _, child := range current[:limit] {
			result = append(result, sanitizeValue(child, depth+1))
		}
		return result

	case string:
		return truncateString(current, 256)

	case nil, bool, float64:
		return current

	default:
		return "[unsupported]"
	}
}

func sensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	if normalized == "sdp_bytes" {
		return false
	}
	if normalized == "ip" || strings.HasSuffix(normalized, "_ip") {
		return true
	}
	for _, fragment := range []string{
		"authorization", "cookie", "credential", "password", "secret",
		"token", "content", "message", "sdp", "candidate", "address",
	} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

func truncateString(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	if maximum <= 3 {
		return strings.Repeat(".", maximum)
	}
	value = value[:maximum-3]
	for !utf8.ValidString(value) {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value + "..."
}
