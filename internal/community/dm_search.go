package community

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
)

// DirectSearchResult is one hit of the cross-conversation search.
type DirectSearchResult struct {
	ID             int64  `json:"id"`
	ConversationID int64  `json:"conversation_id"`
	AuthorID       int64  `json:"author_id"`
	AuthorUsername string `json:"author_username"`
	Content        string `json:"content"`
	CreatedAt      int64  `json:"created_at"`
}

// searchConversations looks through every conversation the caller belongs to.
// The query is bound as a parameter and matched with the same function as the
// per-conversation search, so the behaviour is identical.
func (s *Service) searchConversations(r *http.Request, userID int64) (any, error) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" || len([]rune(query)) > 200 {
		return nil, ErrInvalid
	}
	cursor := int64(1<<63 - 1)
	if raw := r.URL.Query().Get("before"); raw != "" {
		if _, err := fmt.Sscan(raw, &cursor); err != nil || cursor <= 0 {
			return nil, ErrInvalid
		}
	}
	tx, err := s.db.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(r.Context(), `SELECT m.id,m.conversation_id,m.author_user_id,u.username,m.content,m.created_at
 FROM conversation_messages m
 JOIN conversation_members cm ON cm.conversation_id=m.conversation_id AND cm.user_id=?
 JOIN users u ON u.id=m.author_user_id
 WHERE m.id<? AND voxhold_contains(m.content||' '||m.note,?)
 ORDER BY m.id DESC LIMIT 51`, userID, cursor, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []DirectSearchResult{}
	for rows.Next() {
		var v DirectSearchResult
		if err = rows.Scan(&v.ID, &v.ConversationID, &v.AuthorID, &v.AuthorUsername, &v.Content, &v.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	more := len(results) > 50
	var next *int64
	if more {
		results = results[:50]
		next = &results[len(results)-1].ID
	}
	return map[string]any{"results": results, "has_more": more, "next_before": next}, nil
}
