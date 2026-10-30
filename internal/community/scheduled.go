package community

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"voxhold-backend/internal/message"
	"voxhold-backend/internal/storage"
)

type Scheduled struct {
	Attempts      int         `json:"attempts"`
	NextAttemptAt int64       `json:"next_attempt_at"`
	ID            int64       `json:"id"`
	Destination   Destination `json:"destination"`
	Payload       DirectInput `json:"payload"`
	SendAt        int64       `json:"send_at"`
	Status        string      `json:"status"`
	Error         string      `json:"error"`
}

func (s *Service) scheduled(r *http.Request, uid int64) (any, error) {
	p, err := parsePage(r)
	if err != nil {
		return nil, err
	}
	status := r.URL.Query().Get("status")
	if status != "" && status != "pending" && status != "sent" && status != "failed" && status != "cancelled" {
		return nil, ErrInvalid
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT id,destination_kind,destination_id,payload,send_at,status,error,attempts,next_attempt_at FROM scheduled_messages
 WHERE user_id=? AND (?='' OR status=?) AND (?=0 OR send_at<? OR (send_at=? AND id<?)) ORDER BY send_at DESC,id DESC LIMIT ?`, uid, status, status, p.id, p.before, p.before, p.id, p.limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []Scheduled{}
	for rows.Next() {
		var v Scheduled
		var data string
		if err = rows.Scan(&v.ID, &v.Destination.Kind, &v.Destination.ID, &data, &v.SendAt, &v.Status, &v.Error, &v.Attempts, &v.NextAttemptAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(data), &v.Payload); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	more := len(values) > p.limit
	if more {
		values = values[:p.limit]
	}
	if !p.paged {
		return values, nil
	}
	var next any
	if more {
		last := values[len(values)-1]
		next = pageCursor(last.SendAt, last.ID)
	}
	return map[string]any{"scheduled_messages": values, "has_more": more, "next_cursor": next}, nil
}
func (s *Service) validateSchedule(ctx context.Context, uid int64, v *Scheduled) error {
	if v.SendAt <= time.Now().Unix() || v.SendAt > time.Now().AddDate(1, 0, 0).Unix() {
		return ErrInvalid
	}
	if _, err := s.destination(ctx, uid, v.Destination); err != nil {
		return err
	}
	normalized := (message.CreateInput{Content: v.Payload.Content, ClientMessageID: v.Payload.ClientMessageID, AttachmentIDs: v.Payload.AttachmentIDs, Poll: v.Payload.Poll}).Normalize()
	v.Payload.Content = normalized.Content
	if err := normalized.Validate(); err != nil {
		return ErrInvalid
	}
	if v.Payload.Poll != nil && v.Payload.Poll.ClosesAt <= v.SendAt {
		return ErrInvalid
	}
	return validateScheduleAttachments(ctx, s.db, uid, v)
}

func validateScheduleAttachments(ctx context.Context, q queryer, uid int64, v *Scheduled) error {
	for _, id := range v.Payload.AttachmentIDs {
		var valid bool
		column := "channel_id"
		if v.Destination.Kind == "conversation" {
			column = "conversation_id"
		}
		err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM attachments WHERE id=? AND owner_user_id=? AND `+column+`=? AND message_id IS NULL AND direct_message_id IS NULL AND deleted_at IS NULL)`, id, uid, v.Destination.ID).Scan(&valid)
		if err != nil {
			return err
		}
		if !valid {
			return ErrInvalid
		}
	}
	return nil
}
func (s *Service) schedule(r *http.Request, uid int64) (any, error) {
	var v Scheduled
	if err := decode(r, &v); err != nil {
		return nil, err
	}
	if len(v.Payload.ClientMessageID) > 64 {
		return nil, ErrInvalid
	}
	if v.Payload.Poll != nil {
		v.Payload.Poll.Question = strings.TrimSpace(v.Payload.Poll.Question)
		for i := range v.Payload.Poll.Options {
			v.Payload.Poll.Options[i] = strings.TrimSpace(v.Payload.Poll.Options[i])
		}
	}
	normalized := (message.CreateInput{Content: v.Payload.Content, ClientMessageID: v.Payload.ClientMessageID, AttachmentIDs: v.Payload.AttachmentIDs, Poll: v.Payload.Poll}).Normalize()
	v.Payload.Content = normalized.Content
	s.workMu.Lock()
	defer s.workMu.Unlock()
	if v.Payload.ClientMessageID != "" {
		var existing Scheduled
		var payload string
		err := s.db.QueryRowContext(r.Context(), `SELECT id,destination_kind,destination_id,payload,send_at,status,error FROM scheduled_messages WHERE user_id=? AND json_extract(payload,'$.client_message_id')=?`, uid, v.Payload.ClientMessageID).Scan(&existing.ID, &existing.Destination.Kind, &existing.Destination.ID, &payload, &existing.SendAt, &existing.Status, &existing.Error)
		if err == nil {
			requested, _ := json.Marshal(v.Payload)
			if existing.Destination.Kind != v.Destination.Kind || existing.Destination.ID != v.Destination.ID || existing.SendAt != v.SendAt || string(requested) != payload {
				return nil, ErrConflict
			}
			if err = json.Unmarshal([]byte(payload), &existing.Payload); err != nil {
				return nil, err
			}
			return existing, nil
		}
		if err != sql.ErrNoRows {
			return nil, err
		}
	}
	if err := s.validateSchedule(r.Context(), uid, &v); err != nil {
		return nil, err
	}
	data, _ := json.Marshal(v.Payload)
	v.Status = "pending"
	tx, err := storage.BeginWrite(r.Context(), s.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeActor(r.Context(), tx, uid); err != nil {
		return nil, err
	}
	if err = validateScheduleAttachments(r.Context(), tx, uid, &v); err != nil {
		return nil, err
	}
	err = tx.QueryRowContext(r.Context(), `INSERT INTO scheduled_messages(user_id,destination_kind,destination_id,payload,send_at) VALUES(?,?,?,?,?) RETURNING id`, uid, v.Destination.Kind, v.Destination.ID, string(data), v.SendAt).Scan(&v.ID)
	if err == nil {
		err = tx.Commit()
	}
	if err == nil {
		s.emit(uid, "conversation.scheduled", v)
	}
	return v, err
}
func (s *Service) reschedule(r *http.Request, uid int64) (any, error) {
	var v Scheduled
	if err := decode(r, &v); err != nil {
		return nil, err
	}
	s.workMu.Lock()
	defer s.workMu.Unlock()

	// The creation key belongs to the job, even when its text/time changes.
	// Accept clients omitting it on PUT, but never let edits steal another key.
	var originalPayload string
	if err := s.db.QueryRowContext(r.Context(), `SELECT payload FROM scheduled_messages WHERE id=? AND user_id=? AND status IN('pending','failed')`, path(r, "scheduleID"), uid).Scan(&originalPayload); err != nil {
		return nil, err
	}
	var original DirectInput
	if err := json.Unmarshal([]byte(originalPayload), &original); err != nil {
		return nil, err
	}
	if v.Payload.ClientMessageID != "" && v.Payload.ClientMessageID != original.ClientMessageID {
		return nil, ErrConflict
	}
	v.Payload.ClientMessageID = original.ClientMessageID
	if err := s.validateSchedule(r.Context(), uid, &v); err != nil {
		return nil, err
	}
	data, _ := json.Marshal(v.Payload)
	tx, err := storage.BeginWrite(r.Context(), s.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeActor(r.Context(), tx, uid); err != nil {
		return nil, err
	}
	if err = validateScheduleAttachments(r.Context(), tx, uid, &v); err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(r.Context(), `UPDATE scheduled_messages SET payload=?,destination_kind=?,destination_id=?,send_at=?,status='pending',error='',attempts=0,next_attempt_at=0 WHERE id=? AND user_id=? AND status IN('pending','failed')`, string(data), v.Destination.Kind, v.Destination.ID, v.SendAt, path(r, "scheduleID"), uid)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return nil, sql.ErrNoRows
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	v.ID = path(r, "scheduleID")
	v.Status = "pending"
	s.emit(uid, "conversation.scheduled", v)
	return v, nil
}
func (s *Service) cancelSchedule(r *http.Request, uid int64) (any, error) {
	s.workMu.Lock()
	defer s.workMu.Unlock()
	res, err := s.db.ExecContext(r.Context(), `UPDATE scheduled_messages SET status='cancelled' WHERE id=? AND user_id=? AND status IN('pending','failed')`, path(r, "scheduleID"), uid)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return nil, sql.ErrNoRows
	}
	s.emit(uid, "conversation.scheduled", map[string]bool{"changed": true})
	return map[string]bool{"ok": true}, nil
}

// Run owns no detached goroutines. Stable message keys recover interrupted dispatches without duplicate messages.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			work, cancel := context.WithTimeout(ctx, 10*time.Second)
			if err := s.ProcessDue(work, time.Now().Unix()); err != nil && ctx.Err() == nil {
				log.Printf("community worker: %v", err)
			}
			cancel()
		}
	}
}
func (s *Service) ProcessDue(ctx context.Context, now int64) (resultErr error) {
	// Reminders are independent of message dispatch failures.
	defer func() { resultErr = errors.Join(resultErr, s.processEventReminders(ctx, now)) }()
	s.workMu.Lock()
	defer s.workMu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT id,user_id,destination_kind,destination_id,payload,attempts FROM scheduled_messages WHERE status='pending' AND send_at<=? AND next_attempt_at<=? ORDER BY send_at,id LIMIT 20`, now, now)
	if err != nil {
		return err
	}
	type work struct {
		id, uid, destination int64
		attempts             int
		kind, data           string
	}
	pending := []work{}
	for rows.Next() {
		var v work
		if err = rows.Scan(&v.id, &v.uid, &v.kind, &v.destination, &v.data, &v.attempts); err != nil {
			break
		}
		pending = append(pending, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range pending {
		var input DirectInput
		if err = json.Unmarshal([]byte(v.data), &input); err != nil {
			if _, err = s.db.ExecContext(ctx, `UPDATE scheduled_messages SET status='failed',error='Некорректное задание',attempts=attempts+1 WHERE id=?`, v.id); err != nil {
				return err
			}
			continue
		}
		input.ClientMessageID = fmt.Sprintf("schedule-%d", v.id)
		var delivered bool
		if v.kind == "channel" {
			err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM message_send_keys WHERE channel_id=? AND author_user_id=? AND client_message_id=?)`, v.destination, v.uid, input.ClientMessageID).Scan(&delivered)
		} else {
			err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_messages WHERE conversation_id=? AND author_user_id=? AND client_message_id=?)`, v.destination, v.uid, input.ClientMessageID).Scan(&delivered)
		}
		if err != nil {
			return err
		}
		if delivered {
			if _, err = s.db.ExecContext(ctx, `UPDATE scheduled_messages SET status='sent',error='' WHERE id=?`, v.id); err != nil {
				return err
			}
			continue
		}
		sid, sendErr := s.destination(ctx, v.uid, Destination{Kind: v.kind, ID: v.destination})
		if sendErr == nil {
			if v.kind == "conversation" {
				_, sendErr = s.createDirect(ctx, v.destination, v.uid, input)
			} else if s.messages != nil {
				_, sendErr = s.messages.Create(ctx, sid, v.destination, v.uid, message.CreateInput{Content: input.Content, ClientMessageID: input.ClientMessageID, AttachmentIDs: input.AttachmentIDs, Poll: input.Poll})
			} else {
				sendErr = errors.New("message service unavailable")
			}
		}
		status, problem := "sent", ""
		nextAttempt := int64(0)
		if sendErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			status = "failed"
			problem = "Отправка недоступна: проверьте права, получателей и вложения"
			if !permanentScheduleError(sendErr) && v.attempts+1 < 5 {
				status = "pending"
				nextAttempt = now + int64(5<<min(v.attempts, 4))
				problem = "Временная ошибка отправки; ожидается повторная попытка"
			}
			log.Printf("scheduled message id=%d attempt=%d status=%s: %v", v.id, v.attempts+1, status, sendErr)
		}
		if _, err = s.db.ExecContext(ctx, `UPDATE scheduled_messages SET status=?,error=?,attempts=attempts+1,next_attempt_at=? WHERE id=? AND status='pending'`, status, problem, nextAttempt, v.id); err != nil {
			return err
		}
		s.emit(v.uid, "conversation.scheduled", map[string]any{"id": v.id, "status": status, "error": problem})
	}
	return nil
}

func permanentScheduleError(err error) bool {
	for _, permanent := range []error{ErrForbidden, ErrInvalid, ErrConflict, sql.ErrNoRows,
		message.ErrForbidden, message.ErrPollInvalid, message.ErrContentRequired, message.ErrContentTooLong,
		message.ErrContentInvalid, message.ErrChannelNotFound, message.ErrReplyInvalid, message.ErrClientMessageIDInvalid,
		message.ErrIdempotencyConflict, message.ErrMessageNotFound, message.ErrTextChannelRequired} {
		if errors.Is(err, permanent) {
			return true
		}
	}
	var code interface{ Code() int }
	return errors.As(err, &code) && code.Code()&0xff == 19 // SQLITE_CONSTRAINT cannot succeed by retrying.
}
