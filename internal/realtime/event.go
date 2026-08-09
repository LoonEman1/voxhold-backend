package realtime

import "encoding/json"

const ProtocolVersion = 1

type EventType string

const (
	EventAuthenticate EventType = "auth"
	EventReady        EventType = "ready"
	EventError        EventType = "error"

	EventChannelSubscribe    EventType = "channel.subscribe"
	EventChannelSubscribed   EventType = "channel.subscribed"
	EventChannelUnsubscribe  EventType = "channel.unsubscribe"
	EventChannelUnsubscribed EventType = "channel.unsubscribed"

	EventMessageCreated EventType = "message.created"
	EventMessageUpdated EventType = "message.updated"
	EventMessageDeleted EventType = "message.deleted"
)

type IncomingEvent struct {
	RequestID string          `json:"request_id,omitempty"`
	Type      EventType       `json:"type"`
	Data      json.RawMessage `json:"data,omitempty"`
}

type OutgoingEvent struct {
	RequestID string    `json:"request_id,omitempty"`
	Type      EventType `json:"type"`
	Data      any       `json:"data,omitempty"`
}

type AuthenticateData struct {
	Token string `json:"token"`
}

type ReadyData struct {
	UserID          int64 `json:"user_id"`
	ProtocolVersion int   `json:"protocol_version"`
}

type ChannelSubscriptionData struct {
	ServerID  int64 `json:"server_id"`
	ChannelID int64 `json:"channel_id"`
}

type ErrorCode string

const (
	ErrorInvalidEvent   ErrorCode = "invalid_event"
	ErrorInvalidPayload ErrorCode = "invalid_payload"
	ErrorUnauthorized   ErrorCode = "unauthorized"
	ErrorForbidden      ErrorCode = "forbidden"
	ErrorInternal       ErrorCode = "internal_error"
)

type ErrorData struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

type MessageAuthorData struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
}

type MessageCreatedData struct {
	ID        int64             `json:"id"`
	ChannelID int64             `json:"channel_id"`
	Author    MessageAuthorData `json:"author"`
	Content   string            `json:"content"`
	CreatedAt int64             `json:"created_at"`
	EditedAt  *int64            `json:"edited_at"`
}
