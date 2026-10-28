package notifications

import (
	"context"
	"database/sql"
	"net/http"
	"voxhold-backend/internal/httpapi"
)

type Count struct {
	ChannelID   int64 `json:"channel_id"`
	UnreadCount int64 `json:"unread_count"`
}

func (s *Service) Counts(ctx context.Context, serverID, userID int64) ([]Count, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var member bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM server_members WHERE server_id=? AND user_id=?)`, serverID, userID).Scan(&member); err != nil {
		return nil, err
	}
	if !member {
		return nil, ErrForbidden
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.id,(SELECT count(*) FROM channel_notifications n WHERE n.channel_id=c.id AND n.user_id=? AND n.message_id>COALESCE((SELECT last_read_message_id FROM channel_reads r WHERE r.channel_id=c.id AND r.user_id=n.user_id),0)) FROM channels c WHERE c.server_id=? AND EXISTS(SELECT 1 FROM channel_accessible_members a WHERE a.channel_id=c.id AND a.user_id=?) ORDER BY c.id`, userID, serverID, userID)
	if err != nil {
		return nil, err
	}
	values := []Count{}
	for rows.Next() {
		var value Count
		if err = rows.Scan(&value.ChannelID, &value.UnreadCount); err != nil {
			break
		}
		values = append(values, value)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	return values, tx.Commit()
}

func (s *Service) handleCounts(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	serverID, ok := httpapi.PositiveInt64PathValue(w, r, "serverID", "server")
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	values, err := s.Counts(r.Context(), serverID, userID)
	if err != nil {
		status := http.StatusInternalServerError
		description := "internal server error"
		if err == ErrForbidden {
			status = http.StatusForbidden
			description = err.Error()
		}
		httpapi.WriteError(w, status, description)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, values)
}
