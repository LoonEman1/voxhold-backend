package attachments

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"strings"
	"time"
	"voxhold-backend/internal/storage"
	"voxhold-backend/internal/storagebudget"
)

// Cleanup reclaims abandoned uploads. A database tombstone arbitrates with
// sending and scheduling before any physical content is removed.
func (s *Service) Cleanup(ctx context.Context) error {
	entries, err := fs.ReadDir(s.root.FS(), ".")
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		id := strings.TrimSuffix(name, ".part")
		if !validID(id) || entry.IsDir() {
			continue
		}
		s.mu.Lock()
		storagebudget.Mu.Lock()
		err = s.cleanupEntry(ctx, name, id, cutoff)
		storagebudget.Mu.Unlock()
		s.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) cleanupEntry(ctx context.Context, name, id string, cutoff time.Time) error {
	// Upload/copy publication holds the same file locks. Recheck age after locking.
	info, err := s.root.Stat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.ModTime().Before(cutoff) {
		return nil
	}
	if strings.HasSuffix(name, ".part") {
		return removeMissingOK(s.root, name)
	}
	claimed, err := s.claimAbandoned(ctx, id)
	if err != nil || !claimed {
		return err
	}
	return removeMissingOK(s.root, name)
}

func removeMissingOK(root *os.Root, name string) error {
	err := root.Remove(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Service) claimAbandoned(ctx context.Context, id string) (bool, error) {
	tx, err := storage.BeginWrite(ctx, s.db)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var claimed string
	err = tx.QueryRowContext(ctx, `UPDATE attachments SET deleted_at=COALESCE(deleted_at,unixepoch())
 WHERE id=? AND (deleted_at IS NOT NULL OR (message_id IS NULL AND direct_message_id IS NULL
 AND NOT EXISTS(SELECT 1 FROM scheduled_messages s,json_each(s.payload,'$.attachment_ids') j WHERE s.status='pending' AND j.value=attachments.id))) RETURNING id`, id).Scan(&claimed)
	if err == sql.ErrNoRows {
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM attachments WHERE id=?)`, id).Scan(&exists); err != nil {
			return false, err
		}
		if exists {
			return false, nil
		}
	} else if err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
