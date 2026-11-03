package message

import (
	"errors"
	"fmt"
	"time"
)

// SlowmodeError reports how long the author still has to wait.
type SlowmodeError struct{ RetryAfter time.Duration }

func (e *SlowmodeError) Error() string {
	return fmt.Sprintf("slowmode: wait %d seconds before sending another message", int(e.RetryAfter.Round(time.Second)/time.Second))
}

// IsSlowmode extracts the retry delay from an error chain.
func IsSlowmode(err error) (time.Duration, bool) {
	var slow *SlowmodeError
	if errors.As(err, &slow) {
		return slow.RetryAfter, true
	}
	return 0, false
}
