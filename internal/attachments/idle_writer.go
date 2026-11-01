package attachments

import (
	"net/http"
	"time"
)

// idleDeadlineWriter extends the write deadline per chunk so slow clients can
// finish large files while a stalled connection still fails within idle.
type idleDeadlineWriter struct {
	http.ResponseWriter
	idle time.Duration
}

func (w idleDeadlineWriter) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		n := min(len(p), 64<<10)
		_ = http.NewResponseController(w.ResponseWriter).SetWriteDeadline(time.Now().Add(w.idle))
		count, err := w.ResponseWriter.Write(p[:n])
		written += count
		if err != nil {
			return written, err
		}
		p = p[n:]
	}
	return written, nil
}
