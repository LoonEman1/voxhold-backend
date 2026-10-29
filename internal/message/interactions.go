package message

import (
	"context"
	"errors"
	"slices"
	"strings"
)

var (
	ErrReplyInvalid    = errors.New("reply target must exist in the same channel")
	ErrReactionInvalid = errors.New("unsupported reaction")
	ErrReactionLimit   = errors.New("this message already has twenty different reactions")
	ErrForwardEdit     = errors.New("forwarded messages cannot be edited")
	ErrForwardInvalid  = errors.New("forward source requires positive server, channel and message identifiers")
)

// ReactionEmoji is the shared native-emoji catalog; no arbitrary payloads or URLs.
var ReactionEmoji = strings.Fields("❤️ 👍 👎 😂 🤣 🔥 🤔 😁 😢 😭 😮 😱 🤯 🥰 😍 😎 🤩 🥳 😇 🙂 🙃 😉 😊 😋 😘 😴 🤤 🤨 😐 😑 😏 😒 😔 😕 🙁 ☹️ 😫 😩 😤 😡 🤬 😳 🥺 🥹 🤗 🤫 🤭 🫡 🫠 🤡 💀 👻 👀 🧠 💯 ✅ ❌ ⚡ ⭐ 🌟 🎉 🎊 🎁 💎 🏆 🥇 🚀 🌈 ☀️ 🌙 ☕ 🍿 🍰 🍕 🍓 🍉 🌹 💐 🦋 🐱 🐶 🐸 🦄 🐝 🤝 👏 🙌 👐 🤲 🙏 💪 ✊ 👊 ✌️ 🤞 🤟 🤘 👌 🤌 🫶 🫰 👋")

func ValidReaction(emoji string) bool { return slices.Contains(ReactionEmoji, emoji) }

type ReplyReference struct {
	MessageID int64  `json:"message_id"`
	Author    Author `json:"author"`
	Content   string `json:"content"`
	Revision  int64  `json:"revision"`
	Deleted   bool   `json:"deleted"`
}

// ForwardReference is an explicit exported snapshot. Private source channel
// identifiers are never exposed to recipients of the destination channel.
type ForwardReference struct {
	Author       Author           `json:"author"`
	CreatedAt    int64            `json:"created_at"`
	MentionNames map[int64]string `json:"mention_names,omitempty"`
}

type ForwardInput struct {
	SourceConversationID int64    `json:"source_conversation_id,omitempty"`
	AttachmentIDs        []string `json:"attachment_ids,omitempty"`
	SourceServerID       int64    `json:"source_server_id"`
	SourceChannelID      int64    `json:"source_channel_id"`
	SourceMessageID      int64    `json:"source_message_id"`
	ClientMessageID      string   `json:"client_message_id"`
}

type Reaction struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
	Me    bool   `json:"me"`
}

type ReactionsData struct {
	ChannelID int64      `json:"channel_id"`
	MessageID int64      `json:"message_id"`
	Revision  int64      `json:"revision"`
	Reactions []Reaction `json:"reactions"`
}

func (d ReactionsData) EventChannelID() int64 { return d.ChannelID }

// ReactionState stays inside the backend. Only counts and the recipient's own
// selection are broadcast; names are fetched through an authorized paged API.
type ReactionState struct {
	ChannelID, MessageID, Revision int64
	Users                          map[string]map[int64]struct{}
}

func (s ReactionState) ForUser(userID int64) ReactionsData {
	result := ReactionsData{ChannelID: s.ChannelID, MessageID: s.MessageID, Revision: s.Revision, Reactions: []Reaction{}}
	for _, emoji := range ReactionEmoji {
		users := s.Users[emoji]
		if len(users) == 0 {
			continue
		}
		_, me := users[userID]
		result.Reactions = append(result.Reactions, Reaction{Emoji: emoji, Count: len(users), Me: me})
	}
	return result
}

type ReactorPage struct {
	Users      []Author `json:"users"`
	HasMore    bool     `json:"has_more"`
	NextUserID *int64   `json:"next_user_id"`
}

type InteractionRepository interface {
	CreateReferenced(context.Context, int64, int64, int64, CreateInput, *ForwardInput) (Message, error)
	SetReaction(context.Context, int64, int64, int64, int64, string, bool) (ReactionState, bool, error)
	ListReactors(context.Context, int64, int64, int64, int64, string, int64, int) (ReactorPage, error)
}

func (s *Service) Forward(ctx context.Context, serverID, channelID, userID int64, input ForwardInput) (Message, error) {
	if serverID <= 0 || channelID <= 0 {
		return Message{}, ErrChannelNotFound
	}
	if userID <= 0 {
		return Message{}, ErrForbidden
	}
	if (input.SourceConversationID <= 0 && (input.SourceServerID <= 0 || input.SourceChannelID <= 0)) || input.SourceMessageID <= 0 {
		return Message{}, ErrForwardInvalid
	}
	if err := (CreateInput{Content: "forward", ClientMessageID: input.ClientMessageID, AttachmentIDs: input.AttachmentIDs}).Validate(); err != nil {
		return Message{}, err
	}
	repo, ok := s.repository.(InteractionRepository)
	if !ok {
		return Message{}, errors.New("message interactions unavailable")
	}
	value, err := repo.CreateReferenced(ctx, serverID, channelID, userID, CreateInput{ClientMessageID: input.ClientMessageID, AttachmentIDs: input.AttachmentIDs}, &input)
	if err != nil {
		return Message{}, err
	}
	s.events.PublishMessageCreated(value)
	return value, nil
}

func (s *Service) SetReaction(ctx context.Context, serverID, channelID, messageID, userID int64, emoji string, active bool) (ReactionsData, error) {
	if !ValidReaction(emoji) {
		return ReactionsData{}, ErrReactionInvalid
	}
	repo, ok := s.repository.(InteractionRepository)
	if !ok {
		return ReactionsData{}, errors.New("message interactions unavailable")
	}
	state, changed, err := repo.SetReaction(ctx, serverID, channelID, messageID, userID, emoji, active)
	if err != nil {
		return ReactionsData{}, err
	}
	if changed {
		if publisher, ok := s.events.(interface{ PublishReactionsChanged(ReactionState) }); ok {
			publisher.PublishReactionsChanged(state)
		}
	}
	return state.ForUser(userID), nil
}

func (s *Service) ListReactors(ctx context.Context, serverID, channelID, messageID, userID int64, emoji string, after int64, limit int) (ReactorPage, error) {
	if !ValidReaction(emoji) {
		return ReactorPage{}, ErrReactionInvalid
	}
	repo, ok := s.repository.(InteractionRepository)
	if !ok {
		return ReactorPage{}, errors.New("message interactions unavailable")
	}
	return repo.ListReactors(ctx, serverID, channelID, messageID, userID, emoji, max(0, after), min(100, max(1, limit)))
}
