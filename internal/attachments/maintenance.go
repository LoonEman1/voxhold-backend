package attachments

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"strings"
	"time"
)

// Cleanup reclaims abandoned uploads and files orphaned by message/channel or
// account deletion. A 24-hour grace period excludes all in-flight requests.
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
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		var messageID sql.NullInt64
		var deleted sql.NullInt64
		err = s.db.QueryRowContext(ctx, `SELECT COALESCE(message_id,direct_message_id),deleted_at FROM attachments WHERE id=?`, id).Scan(&messageID, &deleted)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		var scheduled bool
		if err == nil && !messageID.Valid && !deleted.Valid {
			if e := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM scheduled_messages s,json_each(s.payload,'$.attachment_ids') j WHERE s.status='pending' AND j.value=?)`, id).Scan(&scheduled); e != nil {
				return e
			}
		}
		if err == nil && (messageID.Valid || scheduled) && !deleted.Valid {
			continue
		}
		if err = s.root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if _, err = s.db.ExecContext(ctx, `UPDATE attachments SET deleted_at=unixepoch() WHERE id=? AND message_id IS NULL AND direct_message_id IS NULL`, id); err != nil {
			return err
		}
	}
	return nil
}
