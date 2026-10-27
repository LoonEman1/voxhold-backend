package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"voxhold-backend/internal/message"
)

func messagePointers(values []message.Message) []*message.Message {
	result := make([]*message.Message, len(values))
	for index := range values {
		result[index] = &values[index]
	}
	return result
}

func pinPointers(values []message.PinnedMessage) []*message.Message {
	result := make([]*message.Message, len(values))
	for index := range values {
		result[index] = &values[index].Message
	}
	return result
}

func searchPointers(values []message.SearchResult) []*message.Message {
	result := make([]*message.Message, len(values))
	for index := range values {
		result[index] = &values[index].Message
	}
	return result
}

// Hydrate a whole page with two indexed queries, within its authorization/read
// transaction. Never make one query per message or export reactor identities.
func hydrateInteractions(ctx context.Context, tx *sql.Tx, userID int64, values []*message.Message) error {
	if len(values) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(values))
	byID := make(map[int64][]*message.Message, len(values))
	for _, value := range values {
		if _, ok := byID[value.ID]; !ok {
			ids = append(ids, value.ID)
		}
		byID[value.ID] = append(byID[value.ID], value)
		value.Reactions = []message.Reaction{}
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT m.id,m.reply_to_original_id,r.id,r.author_user_id,u.username,r.content,r.revision,m.forward_data,m.revision
 FROM messages m LEFT JOIN messages r ON r.id=m.reply_to_message_id AND r.channel_id=m.channel_id
 LEFT JOIN users u ON u.id=r.author_user_id WHERE m.id IN (SELECT value FROM json_each(?))`, string(encoded))
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, revision int64
		var original, target, author, targetRevision sql.NullInt64
		var username, content, forward sql.NullString
		if err = rows.Scan(&id, &original, &target, &author, &username, &content, &targetRevision, &forward, &revision); err != nil {
			break
		}
		var reply *message.ReplyReference
		if original.Valid {
			reply = &message.ReplyReference{MessageID: original.Int64, Author: message.Author{UserID: author.Int64, Username: username.String}, Content: content.String, Revision: targetRevision.Int64, Deleted: !target.Valid}
		}
		var forwarded *message.ForwardReference
		if forward.Valid {
			if err = json.Unmarshal([]byte(forward.String), &forwarded); err != nil {
				break
			}
		}
		for _, value := range byID[id] {
			value.Reply = reply
			value.Forward = forwarded
			value.ReactionsRevision = revision
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.QueryContext(ctx, `SELECT message_id,emoji,COUNT(*),MAX(user_id=?) FROM message_reactions
 WHERE message_id IN (SELECT value FROM json_each(?)) GROUP BY message_id,emoji ORDER BY message_id,emoji`, userID, string(encoded))
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var reaction message.Reaction
		if err = rows.Scan(&id, &reaction.Emoji, &reaction.Count, &reaction.Me); err != nil {
			break
		}
		for _, value := range byID[id] {
			value.Reactions = append(value.Reactions, reaction)
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	return err
}

func requireInteraction(ctx context.Context, tx *sql.Tx, serverID, channelID, messageID, userID int64) error {
	if _, err := requireServerMember(ctx, tx, serverID, userID); err != nil {
		return err
	}
	if err := requireMessageChannel(ctx, tx, serverID, channelID, userID); err != nil {
		return err
	}
	_, err := findMessage(ctx, tx, channelID, messageID)
	return err
}

func (r *Repository) SetReaction(ctx context.Context, serverID, channelID, messageID, userID int64, emoji string, active bool) (message.ReactionState, bool, error) {
	if !message.ValidReaction(emoji) {
		return message.ReactionState{}, false, message.ErrReactionInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return message.ReactionState{}, false, err
	}
	defer tx.Rollback()
	if err = requireInteraction(ctx, tx, serverID, channelID, messageID, userID); err != nil {
		return message.ReactionState{}, false, err
	}
	var result sql.Result
	if active {
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM message_reactions WHERE message_id=? AND emoji=?)`, messageID, emoji).Scan(&exists); err != nil {
			return message.ReactionState{}, false, err
		}
		if !exists {
			var count int
			if err = tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT emoji) FROM message_reactions WHERE message_id=?`, messageID).Scan(&count); err != nil {
				return message.ReactionState{}, false, err
			}
			if count >= 20 {
				return message.ReactionState{}, false, message.ErrReactionLimit
			}
		}
		result, err = tx.ExecContext(ctx, `INSERT INTO message_reactions(message_id,channel_id,server_id,user_id,emoji) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, messageID, channelID, serverID, userID, emoji)
	} else {
		result, err = tx.ExecContext(ctx, `DELETE FROM message_reactions WHERE message_id=? AND emoji=? AND user_id=?`, messageID, emoji, userID)
	}
	if err != nil {
		return message.ReactionState{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return message.ReactionState{}, false, err
	}
	state := message.ReactionState{ChannelID: channelID, MessageID: messageID, Users: map[string]map[int64]struct{}{}}
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM messages WHERE id=?`, messageID).Scan(&state.Revision); err != nil {
		return state, false, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT emoji,user_id FROM message_reactions WHERE message_id=?`, messageID)
	if err != nil {
		return state, false, err
	}
	for rows.Next() {
		var value string
		var id int64
		if err = rows.Scan(&value, &id); err != nil {
			break
		}
		if state.Users[value] == nil {
			state.Users[value] = map[int64]struct{}{}
		}
		state.Users[value][id] = struct{}{}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return state, false, err
	}
	return state, affected != 0, tx.Commit()
}

func (r *Repository) ListReactors(ctx context.Context, serverID, channelID, messageID, userID int64, emoji string, after int64, limit int) (message.ReactorPage, error) {
	page := message.ReactorPage{Users: []message.Author{}}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	if err = requireInteraction(ctx, tx, serverID, channelID, messageID, userID); err != nil {
		return page, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT r.user_id,u.username FROM message_reactions r JOIN users u ON u.id=r.user_id
 WHERE r.message_id=? AND r.emoji=? AND r.user_id>? ORDER BY r.user_id LIMIT ?`, messageID, emoji, after, limit+1)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		var user message.Author
		if err = rows.Scan(&user.UserID, &user.Username); err != nil {
			break
		}
		page.Users = append(page.Users, user)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return page, err
	}
	page.HasMore = len(page.Users) > limit
	if page.HasMore {
		page.Users = page.Users[:limit]
		id := page.Users[len(page.Users)-1].UserID
		page.NextUserID = &id
	}
	return page, tx.Commit()
}
