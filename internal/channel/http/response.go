package channelhttp

import "voxhold-backend/internal/channel"

type channelResponse struct {
	ID        int64        `json:"id"`
	ServerID  int64        `json:"server_id"`
	Name      string       `json:"name"`
	Kind      channel.Kind `json:"kind"`
	Position  int64        `json:"position"`
	CreatedBy int64        `json:"created_by"`
	CreatedAt int64        `json:"created_at"`
}

type listedChannelResponse struct {
	ID            int64        `json:"id"`
	ServerID      int64        `json:"server_id"`
	Name          string       `json:"name"`
	Kind          channel.Kind `json:"kind"`
	Position      int64        `json:"position"`
	CreatedBy     int64        `json:"created_by"`
	CreatedAt     int64        `json:"created_at"`
	LastMessageID int64        `json:"last_message_id"`
}

func newChannelResponse(value channel.Channel) channelResponse {
	return channelResponse{
		ID:        value.ID,
		ServerID:  value.ServerID,
		Name:      value.Name,
		Kind:      value.Kind,
		Position:  value.Position,
		CreatedBy: value.CreatedBy,
		CreatedAt: value.CreatedAt,
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
				ID:            value.ID,
				ServerID:      value.ServerID,
				Name:          value.Name,
				Kind:          value.Kind,
				Position:      value.Position,
				CreatedBy:     value.CreatedBy,
				CreatedAt:     value.CreatedAt,
				LastMessageID: value.LastMessageID,
			},
		)
	}

	return response
}
