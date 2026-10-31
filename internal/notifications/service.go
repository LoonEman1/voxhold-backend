package notifications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"voxhold-backend/internal/mentions"
	"voxhold-backend/internal/message"
	"voxhold-backend/internal/storage"
)

type Mode string

const (
	All      Mode = "all"
	Mentions Mode = "mentions"
	None     Mode = "none"
)

var (
	ErrForbidden    = errors.New("channel notifications are unavailable")
	ErrTextRequired = errors.New("notification settings require a text channel")
	ErrMode         = errors.New("invalid notification mode")
)

type Settings struct {
	ServerID  int64 `json:"server_id"`
	ChannelID int64 `json:"channel_id"`
	Mode      Mode  `json:"mode"`
}

func (s Settings) EventChannelID() int64 { return s.ChannelID }

type Author struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
}
type Preview struct {
	ID        int64  `json:"id"`
	Author    Author `json:"author"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"`
}
type Event struct {
	ServerID    int64   `json:"server_id"`
	ChannelID   int64   `json:"channel_id"`
	ServerName  string  `json:"server_name"`
	ChannelName string  `json:"channel_name"`
	Mentioned   bool    `json:"mentioned"`
	Message     Preview `json:"message"`
}

func (e Event) EventChannelID() int64 { return e.ChannelID }

type Service struct {
	db              *sql.DB
	isOnline        func(int64) bool
	queue           chan message.Message
	publish         func(int64, Event)
	settingsChanged func(int64, Settings)
}

func NewService(db *sql.DB, isOnline func(int64) bool, publish func(int64, Event), changed func(int64, Settings)) *Service {
	return &Service{db: db, isOnline: isOnline, queue: make(chan message.Message, 1024), publish: publish, settingsChanged: changed}
}

func check(ctx context.Context, tx *sql.Tx, serverID, channelID, userID int64) error {
	var kind string
	var allowed bool
	err := tx.QueryRowContext(ctx, `SELECT c.kind,EXISTS(SELECT 1 FROM channel_accessible_members a WHERE a.channel_id=c.id AND a.user_id=?) FROM channels c WHERE c.id=? AND c.server_id=?`, userID, channelID, serverID).Scan(&kind, &allowed)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	if kind != "text" {
		return ErrTextRequired
	}
	return nil
}
func (s *Service) Get(ctx context.Context, serverID, channelID, userID int64) (Settings, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Settings{}, err
	}
	defer tx.Rollback()
	if err := check(ctx, tx, serverID, channelID, userID); err != nil {
		return Settings{}, err
	}
	value := Settings{ServerID: serverID, ChannelID: channelID, Mode: Mentions}
	var news bool
	if err := tx.QueryRowContext(ctx, `SELECT is_news FROM channels WHERE id=?`, channelID).Scan(&news); err != nil {
		return Settings{}, err
	}
	if news {
		value.Mode = All
	}
	err = tx.QueryRowContext(ctx, `SELECT mode FROM channel_notification_settings WHERE channel_id=? AND user_id=?`, channelID, userID).Scan(&value.Mode)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Settings{}, err
	}
	return value, tx.Commit()
}
func (s *Service) Set(ctx context.Context, serverID, channelID, userID int64, mode Mode) (Settings, error) {
	if mode != All && mode != Mentions && mode != None {
		return Settings{}, ErrMode
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Settings{}, err
	}
	defer tx.Rollback()
	if err := check(ctx, tx, serverID, channelID, userID); err != nil {
		return Settings{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_notification_settings(channel_id,server_id,user_id,mode) VALUES(?,?,?,?) ON CONFLICT(channel_id,user_id) DO UPDATE SET mode=excluded.mode`, channelID, serverID, userID, mode)
	if err != nil {
		return Settings{}, err
	}
	if err = tx.Commit(); err != nil {
		return Settings{}, err
	}
	value := Settings{ServerID: serverID, ChannelID: channelID, Mode: mode}
	if s.settingsChanged != nil {
		s.settingsChanged(userID, value)
	}
	return value, nil
}

// Deliver persists eligible alerts for online and offline accounts, then sends
// transient previews to connected accounts. Counts survive reconnects and use
// exactly the same mentions/reply/forward rules as live notifications.
func (s *Service) Deliver(value message.Message) error {
	return s.deliver(context.Background(), value)
}

func (s *Service) deliver(parent context.Context, value message.Message) error {
	if value.Replayed {
		return nil
	}
	targets := mentions.Parse(value.Content)
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var base Event
	var news bool
	var kind string
	err = tx.QueryRowContext(ctx, `SELECT c.kind,c.is_news,c.server_id,c.name,s.name
 FROM channels c JOIN servers s ON s.id=c.server_id WHERE c.id=?`, value.ChannelID).Scan(&kind, &news, &base.ServerID, &base.ChannelName, &base.ServerName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("notification channel: %w", err)
	}
	if kind != "text" {
		return nil
	}
	users := make([]int64, 0, len(targets.Users)+1)
	names := make([]string, 0, len(targets.Names))
	roles := make([]string, 0, len(targets.Roles))
	if value.Forward == nil {
		for id := range targets.Users {
			users = append(users, id)
		}
		for name := range targets.Names {
			names = append(names, name)
		}
		for role := range targets.Roles {
			roles = append(roles, role)
		}
		if value.Reply != nil && !value.Reply.Deleted {
			users = append(users, value.Reply.Author.UserID)
		}
	}
	userJSON, _ := json.Marshal(users)
	nameJSON, _ := json.Marshal(names)
	roleJSON, _ := json.Marshal(roles)
	// Start from indexed candidate sets instead of scanning every channel member
	// for an ordinary message with no mentions. The final ACL/settings predicates
	// still apply inside the same transaction, including explicit opt-outs.
	rows, err := tx.QueryContext(ctx, `WITH candidates(user_id) AS (
 SELECT user_id FROM server_members WHERE server_id=? AND ?
 UNION SELECT user_id FROM channel_notification_settings WHERE channel_id=? AND mode='all' AND ?
 UNION SELECT CAST(value AS INTEGER) FROM json_each(?)
 UNION SELECT id FROM users WHERE username IN (SELECT value FROM json_each(?))
 UNION SELECT user_id FROM server_members WHERE server_id=? AND role IN (SELECT value FROM json_each(?))
 UNION SELECT user_id FROM thread_subscriptions WHERE message_id=? AND following=1
 )
 SELECT u.id,u.username,m.role,COALESCE(n.mode,?),
 EXISTS(SELECT 1 FROM thread_subscriptions t WHERE t.message_id=? AND t.user_id=u.id AND t.following=1)
 FROM candidates candidate CROSS JOIN users u ON u.id=candidate.user_id
 CROSS JOIN server_members m ON m.server_id=? AND m.user_id=u.id
 LEFT JOIN channel_notification_settings n ON n.channel_id=? AND n.user_id=u.id
 WHERE u.id<>? AND u.deleted_at IS NULL
 AND COALESCE(n.mode,?)<>'none'
 AND EXISTS(SELECT 1 FROM channel_accessible_members a WHERE a.channel_id=? AND a.user_id=u.id)
 AND NOT EXISTS(SELECT 1 FROM user_bans WHERE user_id=u.id)`,
		base.ServerID, news && value.ThreadRootID == nil, value.ChannelID, value.ThreadRootID == nil,
		string(userJSON), string(nameJSON), base.ServerID, string(roleJSON), value.ThreadRootID,
		defaultMode(news), value.ThreadRootID, base.ServerID, value.ChannelID, value.Author.UserID, defaultMode(news), value.ChannelID)
	if err != nil {
		return fmt.Errorf("notification recipients: %w", err)
	}
	type delivery struct {
		userID int64
		event  Event
	}
	deliveries := make([]delivery, 0)
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		var username, role string
		var mode Mode
		event := base
		var following bool
		if err = rows.Scan(&id, &username, &role, &mode, &following); err != nil {
			break
		}
		event.Mentioned = value.Forward == nil && (targets.Matches(id, username, role) || value.Reply != nil && !value.Reply.Deleted && value.Reply.Author.UserID == id)
		// A busy discussion should not ping all subscribers of a news feed.
		if (mode != All || value.ThreadRootID != nil) && !event.Mentioned && !following {
			continue
		}
		event.ChannelID = value.ChannelID
		event.Message = Preview{ID: value.ID, Author: Author{UserID: value.Author.UserID, Username: value.Author.Username}, Content: value.Content, CreatedAt: value.CreatedAt}
		deliveries = append(deliveries, delivery{id, event})
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if len(deliveries) == 0 {
		return nil
	}
	inserted, err := s.persistDeliveries(ctx, value.ChannelID, value.ID, ids)
	if err != nil {
		return err
	}
	if s.publish != nil {
		for _, item := range deliveries {
			if inserted[item.userID] && s.isOnline != nil && s.isOnline(item.userID) {
				s.publish(item.userID, item.event)
			}
		}
	}
	return nil
}

func defaultMode(news bool) Mode {
	if news {
		return All
	}
	return Mentions
}

// persistDeliveries revalidates access after the read snapshot is released.
// Returning only inserted IDs prevents phantom events and duplicate alerts.
func (s *Service) persistDeliveries(ctx context.Context, channelID, messageID int64, ids []int64) (map[int64]bool, error) {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	tx, err := storage.BeginWrite(ctx, s.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `INSERT INTO channel_notifications(channel_id,server_id,user_id,message_id)
 SELECT c.id,c.server_id,CAST(j.value AS INTEGER),m.id FROM json_each(?) j
 JOIN messages m ON m.id=? JOIN channels c ON c.id=m.channel_id AND c.id=?
 WHERE m.id>COALESCE((SELECT last_read_message_id FROM channel_reads r WHERE r.channel_id=c.id AND r.user_id=CAST(j.value AS INTEGER)),0)
 AND EXISTS(SELECT 1 FROM channel_accessible_members a WHERE a.channel_id=c.id AND a.user_id=CAST(j.value AS INTEGER))
 AND EXISTS(SELECT 1 FROM users u WHERE u.id=CAST(j.value AS INTEGER) AND u.deleted_at IS NULL)
 AND NOT EXISTS(SELECT 1 FROM user_bans WHERE user_id=CAST(j.value AS INTEGER))
 ON CONFLICT DO NOTHING RETURNING user_id`, string(encoded), messageID, channelID)
	if err != nil {
		return nil, err
	}
	inserted := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		inserted[id] = true
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return inserted, nil
}
