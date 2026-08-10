package readstatehttp

import "voxhold-backend/internal/readstate"

type channelReadResponse struct {
	ServerID          int64 `json:"server_id"`
	ChannelID         int64 `json:"channel_id"`
	UserID            int64 `json:"user_id"`
	LastReadMessageID int64 `json:"last_read_message_id"`
	UpdatedAt         int64 `json:"updated_at"`
}

func newChannelReadResponse(
	read readstate.ChannelRead,
) channelReadResponse {
	return channelReadResponse{
		ServerID:          read.ServerID,
		ChannelID:         read.ChannelID,
		UserID:            read.UserID,
		LastReadMessageID: read.LastReadMessageID,
		UpdatedAt:         read.UpdatedAt,
	}
}
