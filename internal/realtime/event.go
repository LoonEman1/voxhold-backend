package realtime

import "encoding/json"

const ProtocolVersion = 5

type EventType string

const (
	EventAuthenticate EventType = "auth"
	EventReady        EventType = "ready"
	EventError        EventType = "error"

	EventChannelSubscribe    EventType = "channel.subscribe"
	EventChannelSubscribed   EventType = "channel.subscribed"
	EventChannelUnsubscribe  EventType = "channel.unsubscribe"
	EventChannelUnsubscribed EventType = "channel.unsubscribed"

	EventServerMemberRemoved     EventType = "server.member_removed"
	EventServerMemberJoined      EventType = "server.member_joined"
	EventServerMemberRoleUpdated EventType = "server.member_role_updated"
	EventServerDeleted           EventType = "server.deleted"

	EventInvitationReceived EventType = "invitation.received"

	EventChannelCreated EventType = "channel.created"
	EventChannelUpdated EventType = "channel.updated"
	EventChannelDeleted EventType = "channel.deleted"

	EventReadSnapshot        EventType = "read.snapshot"
	EventChannelReadSnapshot EventType = "channel.read_snapshot"
	EventChannelRead         EventType = "channel.read"

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
	ErrorInvalidState   ErrorCode = "invalid_state"
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

type ServerMemberRemovedData struct {
	ServerID int64 `json:"server_id"`
	UserID   int64 `json:"user_id"`
}

type ServerMemberData struct {
	UserID      int64   `json:"user_id"`
	Username    string  `json:"username"`
	CreatedAt   int64   `json:"created_at"`
	Role        string  `json:"role"`
	JoinedAt    int64   `json:"joined_at"`
	About       string  `json:"about"`
	CountryCode *string `json:"country_code"`
	LastSeenAt  *int64  `json:"last_seen_at"`
}

type ServerMemberChangedData struct {
	ServerID int64            `json:"server_id"`
	Member   ServerMemberData `json:"member"`
}

type InvitationReceivedData struct {
	ID              int64  `json:"id"`
	ServerID        int64  `json:"server_id"`
	ServerName      string `json:"server_name"`
	InviterUserID   int64  `json:"inviter_user_id"`
	InviterUsername string `json:"inviter_username"`
	InviteeUserID   int64  `json:"invitee_user_id"`
	Status          string `json:"status"`
	ExpiresAt       int64  `json:"expires_at"`
	CreatedAt       int64  `json:"created_at"`
}

type ChannelData struct {
	ID        int64  `json:"id"`
	ServerID  int64  `json:"server_id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Position  int64  `json:"position"`
	CreatedBy int64  `json:"created_by"`
	CreatedAt int64  `json:"created_at"`
}

type ChannelDeletedData struct {
	ServerID  int64 `json:"server_id"`
	ChannelID int64 `json:"channel_id"`
}

type ChannelReadData struct {
	ServerID          int64 `json:"server_id"`
	ChannelID         int64 `json:"channel_id"`
	UserID            int64 `json:"user_id"`
	LastReadMessageID int64 `json:"last_read_message_id"`
	UpdatedAt         int64 `json:"updated_at"`
}

type ReadSnapshotData struct {
	Reads []ChannelReadData `json:"reads"`
}

type ChannelReadSnapshotData struct {
	ServerID  int64             `json:"server_id"`
	ChannelID int64             `json:"channel_id"`
	Reads     []ChannelReadData `json:"reads"`
}

type ServerDeletedData struct {
	ServerID int64 `json:"server_id"`
}
