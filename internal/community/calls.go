package community

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

type CallMember struct {
	UserID    int64  `json:"user_id"`
	Username  string `json:"username"`
	SessionID string `json:"session_id"`
}
type CallState struct {
	ConversationID int64        `json:"conversation_id"`
	Revision       int64        `json:"revision"`
	Members        []CallMember `json:"members"`
}

func (s *Service) readCall(ctx context.Context, cid int64) (CallState, error) {
	v := CallState{ConversationID: cid, Members: []CallMember{}}
	rows, err := s.db.QueryContext(ctx, `SELECT c.call_revision,m.user_id,u.username,m.session_id FROM conversations c LEFT JOIN conversation_call_members m ON m.conversation_id=c.id AND m.heartbeat_at>? LEFT JOIN users u ON u.id=m.user_id WHERE c.id=? ORDER BY m.user_id`, time.Now().Unix()-45, cid)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var userID sql.NullInt64
		var username, sessionID sql.NullString
		if err = rows.Scan(&v.Revision, &userID, &username, &sessionID); err != nil {
			return v, err
		}
		if userID.Valid {
			v.Members = append(v.Members, CallMember{UserID: userID.Int64, Username: username.String, SessionID: sessionID.String})
		}
	}
	return v, rows.Err()
}
func (s *Service) callState(r *http.Request, uid int64) (any, error) {
	cid := path(r, "conversationID")
	if err := conversationAccess(r.Context(), s.db, cid, uid); err != nil {
		return nil, err
	}
	return s.readCall(r.Context(), cid)
}
func (s *Service) joinCall(r *http.Request, uid int64) (any, error) {
	cid := path(r, "conversationID")
	var input struct {
		SessionID string `json:"session_id"`
		Replace   bool   `json:"replace"`
	}
	if err := decode(r, &input); err != nil {
		return nil, err
	}
	if len(input.SessionID) < 16 || len(input.SessionID) > 64 {
		return nil, ErrInvalid
	}
	if err := CanSend(r.Context(), s.db, cid, uid); err != nil {
		return nil, err
	}
	var saved bool
	if err := s.db.QueryRowContext(r.Context(), `SELECT saved_owner_id IS NOT NULL FROM conversations WHERE id=?`, cid).Scan(&saved); err != nil {
		return nil, err
	}
	if saved {
		return nil, ErrInvalid
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = canSendTransaction(r.Context(), tx, cid, uid); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM conversation_call_members WHERE heartbeat_at<?`, time.Now().Unix()-45); err != nil {
		return nil, err
	}
	var exists bool
	if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM conversation_call_members WHERE user_id=? AND conversation_id<>?)`, uid, cid).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrConflict
	}
	var previous string
	_ = tx.QueryRowContext(r.Context(), `SELECT session_id FROM conversation_call_members WHERE conversation_id=? AND user_id=?`, cid, uid).Scan(&previous)
	if previous != "" && previous != input.SessionID && !input.Replace {
		return nil, ErrConflict
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO conversation_call_members(conversation_id,user_id,session_id) VALUES(?,?,?) ON CONFLICT(conversation_id,user_id) DO UPDATE SET session_id=excluded.session_id,heartbeat_at=unixepoch()`, cid, uid, input.SessionID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	v, err := s.readCall(r.Context(), cid)
	if err != nil {
		return nil, err
	}
	if previous != input.SessionID {
		s.emitConversation(r.Context(), cid, "conversation.call", v)
	}
	return v, nil
}
func (s *Service) leaveCall(r *http.Request, uid int64) (any, error) {
	cid := path(r, "conversationID")
	var input struct {
		SessionID string `json:"session_id"`
	}
	if err := decode(r, &input); err != nil {
		return nil, err
	}
	if err := conversationAccess(r.Context(), s.db, cid, uid); err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM conversation_call_members WHERE conversation_id=? AND user_id=? AND session_id=?`, cid, uid, input.SessionID); err != nil {
		return nil, err
	}
	v, err := s.readCall(r.Context(), cid)
	if err == nil {
		s.emitConversation(r.Context(), cid, "conversation.call", v)
	}
	return v, err
}
func (s *Service) callSignal(r *http.Request, uid int64) (any, error) {
	cid := path(r, "conversationID")
	var input struct {
		TargetID        int64           `json:"target_user_id"`
		SessionID       string          `json:"session_id"`
		TargetSessionID string          `json:"target_session_id"`
		Description     json.RawMessage `json:"description,omitempty"`
		Candidate       json.RawMessage `json:"candidate,omitempty"`
		Media           *struct {
			Screen   bool  `json:"screen"`
			Revision int64 `json:"revision"`
		} `json:"media,omitempty"`
	}
	if err := decode(r, &input); err != nil {
		return nil, err
	}
	count := 0
	if len(input.Description) > 0 {
		count++
	}
	if len(input.Candidate) > 0 {
		count++
	}
	if input.Media != nil {
		count++
	}
	if input.Media != nil && (input.Media.Revision < 0 || input.Media.Revision > 1000000000) {
		return nil, ErrInvalid
	}
	if input.TargetID == uid || input.TargetID <= 0 || len(input.Description) > 64*1024 || len(input.Candidate) > 4096 || count != 1 {
		return nil, ErrInvalid
	}
	if err := conversationAccess(r.Context(), s.db, cid, uid); err != nil {
		return nil, err
	}
	var valid bool
	err := s.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM conversation_call_members a JOIN conversation_call_members b ON b.conversation_id=a.conversation_id WHERE a.conversation_id=? AND a.user_id=? AND a.session_id=? AND b.user_id=? AND b.session_id=? AND a.heartbeat_at>? AND b.heartbeat_at>?)`, cid, uid, input.SessionID, input.TargetID, input.TargetSessionID, time.Now().Unix()-45, time.Now().Unix()-45).Scan(&valid)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, ErrForbidden
	}
	if err = canContact(r.Context(), s.db, uid, input.TargetID); err != nil {
		return nil, err
	}
	s.emit(input.TargetID, "conversation.call-signal", map[string]any{"conversation_id": cid, "from_user_id": uid, "session_id": input.SessionID, "target_session_id": input.TargetSessionID, "description": input.Description, "candidate": input.Candidate, "media": input.Media})
	return map[string]bool{"ok": true}, nil
}
