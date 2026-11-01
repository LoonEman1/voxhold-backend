package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Backup writes a consistent snapshot of a live WAL database to path with
// VACUUM INTO and verifies it with PRAGMA integrity_check. A snapshot that
// fails verification is removed.
func Backup(ctx context.Context, db *sql.DB, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale snapshot: %w", err)
	}
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("snapshot database: %w", err)
	}
	if err := verifySnapshot(ctx, path); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func verifySnapshot(ctx context.Context, path string) error {
	snapshot, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open snapshot: %w", err)
	}
	defer snapshot.Close()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var result string
	if err = snapshot.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("check snapshot: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("snapshot integrity check failed: %s", result)
	}
	return nil
}
