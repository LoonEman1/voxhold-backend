package message

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrPollInvalid = errors.New("invalid poll")

type PollInput struct {
	Question  string   `json:"question"`
	Options   []string `json:"options"`
	Multiple  bool     `json:"multiple"`
	Anonymous bool     `json:"anonymous"`
	ClosesAt  int64    `json:"closes_at"`
}
type PollOption struct {
	ID     int64    `json:"id"`
	Label  string   `json:"label"`
	Count  int      `json:"count"`
	Me     bool     `json:"me"`
	Voters []Author `json:"voters,omitempty"`
}
type Poll struct {
	ID        int64        `json:"id"`
	Question  string       `json:"question"`
	Multiple  bool         `json:"multiple"`
	Anonymous bool         `json:"anonymous"`
	ClosesAt  int64        `json:"closes_at"`
	Revision  int64        `json:"revision"`
	Options   []PollOption `json:"options"`
}

func (p *PollInput) Validate() error {
	if p == nil {
		return nil
	}
	p.Question = strings.TrimSpace(p.Question)
	if utf8.RuneCountInString(p.Question) < 1 || utf8.RuneCountInString(p.Question) > 300 || len(p.Options) < 2 || len(p.Options) > 10 || p.ClosesAt <= time.Now().Unix() {
		return ErrPollInvalid
	}
	seen := map[string]bool{}
	for i, v := range p.Options {
		v = strings.TrimSpace(v)
		if v == "" || utf8.RuneCountInString(v) > 120 || seen[strings.ToLower(v)] {
			return ErrPollInvalid
		}
		seen[strings.ToLower(v)] = true
		p.Options[i] = v
	}
	return nil
}

// StorePoll participates in the message transaction, so failed sends never leave a poll behind.
func StorePoll(ctx context.Context, tx *sql.Tx, kind string, messageID int64, input *PollInput) error {
	if input == nil {
		return nil
	}
	if err := input.Validate(); err != nil {
		return err
	}
	var channel, direct any
	if kind == "channel" {
		channel = messageID
	} else {
		direct = messageID
	}
	var id int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO polls(channel_message_id,direct_message_id,question,multiple,anonymous,closes_at) VALUES(?,?,?,?,?,?) RETURNING id`, channel, direct, input.Question, input.Multiple, input.Anonymous, input.ClosesAt).Scan(&id); err != nil {
		return err
	}
	for _, label := range input.Options {
		if _, err := tx.ExecContext(ctx, `INSERT INTO poll_options(poll_id,label) VALUES(?,?)`, id, label); err != nil {
			return err
		}
	}
	return nil
}

// LoadPolls loads a page in two queries, without exposing voter identities for anonymous polls.
func LoadPolls(ctx context.Context, tx *sql.Tx, userID int64, kind string, ids []int64) (map[int64]*Poll, error) {
	result := map[int64]*Poll{}
	if len(ids) == 0 {
		return result, nil
	}
	column := "channel_message_id"
	if kind == "conversation" {
		column = "direct_message_id"
	}
	encoded, _ := json.Marshal(ids)
	rows, err := tx.QueryContext(ctx, `SELECT id,`+column+`,question,multiple,anonymous,closes_at,revision FROM polls WHERE `+column+` IN (SELECT value FROM json_each(?))`, string(encoded))
	if err != nil {
		return nil, err
	}
	byPoll := map[int64]*Poll{}
	pollIDs := []int64{}
	for rows.Next() {
		p := &Poll{Options: []PollOption{}}
		var mid int64
		if err = rows.Scan(&p.ID, &mid, &p.Question, &p.Multiple, &p.Anonymous, &p.ClosesAt, &p.Revision); err != nil {
			break
		}
		result[mid] = p
		byPoll[p.ID] = p
		pollIDs = append(pollIDs, p.ID)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	encoded, _ = json.Marshal(pollIDs)
	rows, err = tx.QueryContext(ctx, `SELECT o.poll_id,o.id,o.label,COUNT(v.user_id),COALESCE(MAX(v.user_id=?),0),CASE WHEN p.anonymous=0 THEN COALESCE((SELECT json_group_array(json_object('user_id',u.id,'username',u.username)) FROM poll_votes pv JOIN users u ON u.id=pv.user_id WHERE pv.option_id=o.id),'[]') ELSE '[]' END FROM poll_options o JOIN polls p ON p.id=o.poll_id LEFT JOIN poll_votes v ON v.option_id=o.id WHERE o.poll_id IN (SELECT value FROM json_each(?)) GROUP BY o.id ORDER BY o.id`, userID, string(encoded))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pid int64
		var o PollOption
		var voters string
		if err = rows.Scan(&pid, &o.ID, &o.Label, &o.Count, &o.Me, &voters); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(voters), &o.Voters); err != nil {
			break
		}
		byPoll[pid].Options = append(byPoll[pid].Options, o)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	return result, err
}
