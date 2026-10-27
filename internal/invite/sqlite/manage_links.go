package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"voxhold-backend/internal/invite"
)

func requireLinkManager(ctx context.Context, tx *sql.Tx, serverID, userID int64) error {
	var allowed bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM server_members
		WHERE server_id = ? AND user_id = ? AND role IN ('owner', 'admin'))`, serverID, userID).Scan(&allowed); err != nil {
		return fmt.Errorf("check invite link manager: %w", err)
	}
	if !allowed {
		return invite.ErrForbidden
	}
	return nil
}

func (r *Repository) ListActiveLinks(ctx context.Context, serverID, userID int64, beforeID *int64, limit int) ([]invite.InviteLink, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := requireLinkManager(ctx, tx, serverID, userID); err != nil {
		return nil, err
	}
	cursor := int64(1<<63 - 1)
	if beforeID != nil {
		cursor = *beforeID
	}
	rows, err := tx.QueryContext(ctx, `SELECT l.id, l.server_id, s.name, l.created_by, u.username,
		l.expires_at, l.max_uses, l.use_count, l.allow_registration, l.created_at
		FROM server_invite_links l JOIN servers s ON s.id = l.server_id JOIN users u ON u.id = l.created_by
		WHERE l.server_id = ? AND l.id < ? AND l.revoked_at IS NULL AND l.expires_at > unixepoch()
		AND (l.max_uses IS NULL OR l.use_count < l.max_uses)
		ORDER BY l.id DESC LIMIT ?`, serverID, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("query active invite links: %w", err)
	}
	links := make([]invite.InviteLink, 0)
	for rows.Next() {
		var link invite.InviteLink
		if err := rows.Scan(&link.ID, &link.ServerID, &link.ServerName, &link.CreatedBy, &link.CreatorUsername, &link.ExpiresAt, &link.MaxUses, &link.UseCount, &link.AllowRegistration, &link.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		links = append(links, link)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return links, nil
}

func (r *Repository) RevokeLink(ctx context.Context, serverID, linkID, userID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireLinkManager(ctx, tx, serverID, userID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE server_invite_links SET revoked_at = COALESCE(revoked_at, unixepoch()) WHERE id = ? AND server_id = ?`, linkID, serverID)
	if err != nil {
		return fmt.Errorf("revoke invite link row: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return invite.ErrLinkNotFound
	}
	return tx.Commit()
}
