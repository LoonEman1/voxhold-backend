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
	online          func() []int64
	publish         func(int64, Event)
	settingsChanged func(int64, Settings)
}

func NewService(db *sql.DB, online func() []int64, publish func(int64, Event), changed func(int64, Settings)) *Service {
	return &Service{db: db, online: online, publish: publish, settingsChanged: changed}
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

// Deliver targets connected accounts only, independently of their active channel.
// It performs one indexed query, releases the rows before publishing, and never
// expands private-channel delivery beyond the shared channel access policy.
func (s *Service) Deliver(value message.Message) error {
	if value.Replayed || s.online == nil || s.publish == nil {
		return nil
	}
	online := s.online()
	if len(online) == 0 || len(online) == 1 && online[0] == value.Author.UserID {
		return nil
	}
	encoded, err := json.Marshal(online)
	if err != nil {
		return err
	}
	targets := mentions.Parse(value.Content)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT a.user_id,u.username,m.role,COALESCE(n.mode,'mentions'),c.server_id,c.name,s.name
 FROM json_each(?) online
 JOIN channel_accessible_members a ON a.user_id=CAST(online.value AS INTEGER) AND a.channel_id=?
 JOIN channels c ON c.id=a.channel_id AND c.kind='text'
 JOIN servers s ON s.id=c.server_id
 JOIN server_members m ON m.server_id=c.server_id AND m.user_id=a.user_id
 JOIN users u ON u.id=a.user_id
 LEFT JOIN channel_notification_settings n ON n.channel_id=c.id AND n.user_id=a.user_id
 WHERE a.user_id<>? AND COALESCE(n.mode,'mentions')<>'none'`, string(encoded), value.ChannelID, value.Author.UserID)
	if err != nil {
		return fmt.Errorf("notification recipients: %w", err)
	}
	type delivery struct {
		userID int64
		event  Event
	}
	deliveries := make([]delivery, 0)
	for rows.Next() {
		var id int64
		var username, role string
		var mode Mode
		var event Event
		if err = rows.Scan(&id, &username, &role, &mode, &event.ServerID, &event.ChannelName, &event.ServerName); err != nil {
			break
		}
		event.Mentioned = value.Forward == nil && (targets.Matches(id, username, role) || value.Reply != nil && !value.Reply.Deleted && value.Reply.Author.UserID == id)
		if mode != All && !event.Mentioned {
			continue
		}
		event.ChannelID = value.ChannelID
		event.Message = Preview{ID: value.ID, Author: Author{UserID: value.Author.UserID, Username: value.Author.Username}, Content: value.Content, CreatedAt: value.CreatedAt}
		deliveries = append(deliveries, delivery{id, event})
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range deliveries {
		s.publish(item.userID, item.event)
	}
	return nil
}
