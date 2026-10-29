package community

import (
	"net/http"
	"time"
	"voxhold-backend/internal/message"
)

func (s *Service) vote(r *http.Request, uid int64) (any, error) {
	var input struct {
		OptionIDs []int64 `json:"option_ids"`
	}
	if err := decode(r, &input); err != nil {
		return nil, err
	}
	if len(input.OptionIDs) > 10 {
		return nil, ErrInvalid
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	pid := path(r, "pollID")
	kind, mid, scope, closes, err := s.pollAccess(r.Context(), tx, pid, uid)
	if err != nil {
		return nil, err
	}
	if closes <= time.Now().Unix() {
		return nil, ErrConflict
	}
	var multiple bool
	if err = tx.QueryRowContext(r.Context(), `SELECT multiple FROM polls WHERE id=?`, pid).Scan(&multiple); err != nil {
		return nil, err
	}
	if !multiple && len(input.OptionIDs) > 1 {
		return nil, ErrInvalid
	}
	seen := map[int64]bool{}
	for _, id := range input.OptionIDs {
		var valid bool
		if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM poll_options WHERE id=? AND poll_id=?)`, id, pid).Scan(&valid); err != nil {
			return nil, err
		}
		if !valid || seen[id] {
			return nil, ErrInvalid
		}
		seen[id] = true
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM poll_votes WHERE poll_id=? AND user_id=?`, pid, uid); err != nil {
		return nil, err
	}
	for _, id := range input.OptionIDs {
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO poll_votes(poll_id,option_id,user_id) VALUES(?,?,?)`, pid, id, uid); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE polls SET revision=revision+1 WHERE id=?`, pid); err != nil {
		return nil, err
	}
	polls, err := message.LoadPolls(r.Context(), tx, uid, kind, []int64{mid})
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	data := map[string]int64{"poll_id": pid, "message_id": mid}
	if kind == "conversation" {
		data["conversation_id"] = scope
		s.emitConversation(r.Context(), scope, "conversation.poll", data)
	} else {
		s.emitChannel(r.Context(), scope, "community.poll", data)
	}
	return polls[mid], nil
}
