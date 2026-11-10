// Package permissions shares the custom role policy across HTTP and media.
package permissions

import (
	"context"
	"database/sql"
)

const (
	Read           = 1
	Publish        = 2
	Voice          = 4
	Stream         = 8
	ManageMessages = 16
	MuteVoice      = 32
	MoveVoice      = 64
	KickVoice      = 128

	LegacyRoleMask      = Read | Publish | Voice | Stream | ManageMessages
	VoiceModerationMask = MuteVoice | MoveVoice | KickVoice
	RoleMask            = LegacyRoleMask | VoiceModerationMask
)

type Queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func Allowed(ctx context.Context, db Queryer, serverID, userID int64, bit int) (bool, error) {
	var allowed bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM member_permissions WHERE server_id=? AND user_id=? AND (permissions & ?)<>0)`, serverID, userID, bit).Scan(&allowed)
	return allowed, err
}
