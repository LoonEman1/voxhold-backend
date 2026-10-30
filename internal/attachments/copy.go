package attachments

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"voxhold-backend/internal/community"
	"voxhold-backend/internal/storagebudget"
)

// Copy creates an independent destination attachment. Deleting the source does
// not revoke the explicitly forwarded copy; storage quotas include both files.
func (s *Service) Copy(ctx context.Context, uid int64, sourceID, kind string, destination int64, id string) (string, error) {
	if !validID(sourceID) || !validID(id) {
		return "", community.ErrInvalid
	}
	allowed := func() bool {
		if kind == "conversation" {
			return community.CanSend(ctx, s.db, destination, uid) == nil
		}
		return kind == "channel" && s.channel(ctx, destination, uid)
	}
	if !allowed() {
		return "", community.ErrForbidden
	}
	storagebudget.Mu.Lock()
	defer storagebudget.Mu.Unlock()
	var existing string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM attachments WHERE id=? AND owner_user_id=? AND COALESCE(channel_id,0)=? AND COALESCE(conversation_id,0)=? AND deleted_at IS NULL`, id, uid, channelDestination(kind, destination), conversationDestination(kind, destination)).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	var name, mime, category string
	var size int64
	err = s.db.QueryRowContext(ctx, `SELECT a.filename,a.mime_type,a.category,a.size_bytes FROM attachments a WHERE a.id=? AND a.deleted_at IS NULL AND (a.message_id IS NOT NULL OR a.direct_message_id IS NOT NULL) AND ((a.channel_id IS NOT NULL AND EXISTS(SELECT 1 FROM channel_accessible_members WHERE channel_id=a.channel_id AND user_id=?)) OR(a.conversation_id IS NOT NULL AND EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id=a.conversation_id AND user_id=?)))`, sourceID, uid, uid).Scan(&name, &mime, &category, &size)
	if err == sql.ErrNoRows {
		return "", community.ErrForbidden
	}
	if err != nil {
		return "", err
	}
	state, err := storagebudget.Get(ctx, s.db, s.directory)
	if err != nil {
		return "", err
	}
	budget := state.Categories[category]
	var used int64
	for _, v := range state.Categories {
		used += v.Used
	}
	if size > budget.Quota-budget.Used || size > state.Capacity-used {
		return "", storagebudget.ErrBudget
	}
	source, err := s.root.Open(sourceID)
	if err != nil {
		return "", err
	}
	defer source.Close()
	target, err := s.root.Create(id + ".part")
	if err != nil {
		return "", err
	}
	written, copyErr := io.Copy(target, io.LimitReader(source, size+1))
	closeErr := target.Close()
	if copyErr != nil || closeErr != nil || written != size {
		_ = s.root.Remove(id + ".part")
		return "", fmt.Errorf("copy attachment: incomplete content")
	}
	if err = s.root.Rename(id+".part", id); err != nil {
		return "", err
	}
	var cid, conv any
	if kind == "channel" {
		cid = destination
	} else {
		conv = destination
	}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO attachments(id,channel_id,conversation_id,owner_user_id,filename,mime_type,category,size_bytes) VALUES(?,?,?,?,?,?,?,?)`, id, cid, conv, uid, name, mime, category, size); err != nil {
		_ = s.root.Remove(id)
		_ = removeMissingOK(s.root, "thumbs/"+id+".jpg")
		return "", err
	}
	return id, nil
}
func channelDestination(kind string, id int64) int64 {
	if kind == "channel" {
		return id
	}
	return 0
}
func conversationDestination(kind string, id int64) int64 {
	if kind == "conversation" {
		return id
	}
	return 0
}
