package permissions

import (
	"context"
	"database/sql"

	"voxhold-backend/internal/storage"
)

type VoiceModeration struct{ db *sql.DB }

func NewVoiceModeration(db *sql.DB) *VoiceModeration {
	return &VoiceModeration{db: db}
}

func (s *VoiceModeration) Allowed(ctx context.Context, serverID, actorID, targetID int64, bit int) (bool, error) {
	return canModerateVoice(ctx, s.db, serverID, actorID, targetID, bit)
}

// Check both the effective role mask and the built-in role hierarchy.
func canModerateVoice(ctx context.Context, db Queryer, serverID, actorID, targetID int64, bit int) (bool, error) {
	var allowed bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM server_members actor
		JOIN member_permissions p ON p.server_id=actor.server_id AND p.user_id=actor.user_id
		JOIN server_members target ON target.server_id=actor.server_id
		WHERE actor.server_id=? AND actor.user_id=? AND target.user_id=?
		  AND actor.user_id<>target.user_id AND (p.permissions & ?)<>0
		  AND (target.role='member' OR (target.role='admin' AND actor.role='owner'))
	)`, serverID, actorID, targetID, bit).Scan(&allowed)
	return allowed, err
}

func (s *VoiceModeration) Muted(ctx context.Context, serverID, userID int64) (bool, error) {
	var muted bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM server_voice_mutes WHERE server_id=? AND user_id=?)`, serverID, userID).Scan(&muted)
	return muted, err
}

func (s *VoiceModeration) SetMuted(ctx context.Context, serverID, actorID, userID int64, muted bool) (bool, error) {
	tx, err := storage.BeginWrite(ctx, s.db)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	allowed, err := canModerateVoice(ctx, tx, serverID, actorID, userID, MuteVoice)
	if !allowed || err != nil {
		return false, err
	}
	if muted {
		_, err = tx.ExecContext(ctx, `INSERT INTO server_voice_mutes(server_id,user_id) VALUES(?,?) ON CONFLICT DO NOTHING`, serverID, userID)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM server_voice_mutes WHERE server_id=? AND user_id=?`, serverID, userID)
	}
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}
