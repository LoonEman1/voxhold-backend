package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// BeginWrite retries only acquiring a transaction, before any application SQL
// or side effects run. SQLite's busy timeout is not fair under sustained writer
// contention; a short bounded backoff gives a waiting request another turn.
// The existing WAL pool and per-connection busy timeout remain unchanged.
func BeginWrite(ctx context.Context, db *sql.DB) (*sql.Tx, error) {
	return beginWrite(ctx, func() (*sql.Tx, error) { return db.BeginTx(ctx, nil) })
}

func beginWrite(ctx context.Context, begin func() (*sql.Tx, error)) (*sql.Tx, error) {
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tx, err := begin()
		if err == nil {
			return tx, nil
		}
		var code interface{ Code() int }
		if !errors.As(err, &code) || code.Code()&0xff != 5 || attempt >= 2 {
			return nil, err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
