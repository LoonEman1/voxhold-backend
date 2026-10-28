package channelhttp

import "voxhold-backend/internal/channel"

type channelResponse struct {
	CommentsEnabled  bool         `json:"comments_enabled"`
	CanPublish       bool         `json:"can_publish"`
	ID               int64        `json:"id"`
	ServerID         int64        `json:"server_id"`
	Name             string       `json:"name"`
	Kind             channel.Kind `json:"kind"`
	Position         int64        `json:"position"`
	CreatedBy        int64        `json:"created_by"`
	CreatedAt        int64        `json:"created_at"`
	AccessRestricted bool         `json:"access_restricted"`
}

type listedChannelResponse struct {
	CommentsEnabled  bool         `json:"comments_enabled"`
	CanPublish       bool         `json:"can_publish"`
	UnreadCount      int64        `json:"unread_count"`
	ID               int64        `json:"id"`
	ServerID         int64        `json:"server_id"`
	Name             string       `json:"name"`
	Kind             channel.Kind `json:"kind"`
	Position         int64        `json:"position"`
	CreatedBy        int64        `json:"created_by"`
	CreatedAt        int64        `json:"created_at"`
	LastMessageID    int64        `json:"last_message_id"`
	AccessRestricted bool         `json:"access_restricted"`
}

func newChannelResponse(value channel.Channel) channelResponse {
	return channelResponse{
		CanPublish:       value.CanPublish,
		ID:               value.ID,
		ServerID:         value.ServerID,
		Name:             value.Name,
		Kind:             value.Kind,
		Position:         value.Position,
		CreatedBy:        value.CreatedBy,
		CreatedAt:        value.CreatedAt,
		AccessRestricted: value.AccessRestricted,
		CommentsEnabled:  value.CommentsEnabled,
	}
}

func newChannelsResponse(
	values []channel.Channel,
) []listedChannelResponse {
	response := make(
		[]listedChannelResponse,
		0,
		len(values),
	)

	for _, value := range values {
		response = append(
			response,
			listedChannelResponse{
				CanPublish: value.CanPublish, UnreadCount: value.UnreadCount, ID: value.ID,
				ServerID:         value.ServerID,
				Name:             value.Name,
				Kind:             value.Kind,
				Position:         value.Position,
				CreatedBy:        value.CreatedBy,
				CreatedAt:        value.CreatedAt,
				LastMessageID:    value.LastMessageID,
				AccessRestricted: value.AccessRestricted,
				CommentsEnabled:  value.CommentsEnabled,
			},
		)
	}

	return response
}
