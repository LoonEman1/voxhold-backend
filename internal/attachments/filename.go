package attachments

import (
	"strings"
	"unicode"
)

// sanitizeFilename drops control, format (bidi overrides, zero-width) and
// line/paragraph separator characters so a name cannot visually spoof its
// extension, e.g. "photo‮gpj.exe".
func sanitizeFilename(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp) {
			return -1
		}
		return r
	}, name)
	return strings.TrimSpace(cleaned)
}
