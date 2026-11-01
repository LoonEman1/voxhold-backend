package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"voxhold-backend/internal/account"
)

// BackfillUsernameKeys fills username_key for accounts created before the
// column existed. Conflicting names keep a NULL key (and are logged) so the
// startup never fails or renames anyone. It is idempotent.
func BackfillUsernameKeys(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT id, username FROM users WHERE username_key IS NULL`)
	if err != nil {
		return fmt.Errorf("list users without username key: %w", err)
	}
	type pending struct {
		id   int64
		name string
	}
	var todo []pending
	for rows.Next() {
		var p pending
		if err = rows.Scan(&p.id, &p.name); err != nil {
			rows.Close()
			return fmt.Errorf("scan user: %w", err)
		}
		todo = append(todo, p)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, p := range todo {
		var owner int64
		key := account.UsernameKey(p.name)
		err = db.QueryRowContext(ctx,
			`UPDATE users SET username_key=? WHERE id=? AND username_key IS NULL
			 AND NOT EXISTS(SELECT 1 FROM users o WHERE o.username_key=? AND o.id<>?) RETURNING id`,
			key, p.id, key, p.id).Scan(&owner)
		if err == sql.ErrNoRows {
			log.Printf("username key conflict: user %d keeps no key (another account already uses %q)", p.id, key)
			continue
		}
		if err != nil {
			return fmt.Errorf("set username key for user %d: %w", p.id, err)
		}
	}
	return nil
}
