package community

import (
	"database/sql/driver"
	"modernc.org/sqlite"
	"strings"
)

// SQLite's built-in LIKE/NOCASE only fold ASCII. Saved messages and notes need
// case-insensitive Cyrillic search as well, without changing stored content.
func init() {
	sqlite.MustRegisterDeterministicScalarFunction("voxhold_contains", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		text, _ := args[0].(string)
		query, _ := args[1].(string)
		if strings.Contains(strings.ToLower(text), strings.ToLower(query)) {
			return int64(1), nil
		}
		return int64(0), nil
	})
}
