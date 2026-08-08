package serverhttp

import (
	"voxhold-backend/internal/server"
)

type serverResponse struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	CreatedBy int64  `json:"created_by"`
	CreatedAt int64  `json:"created_at"`
}

func newServerResponse(value server.Server) serverResponse {
	return serverResponse{
		ID:        value.ID,
		Name:      value.Name,
		CreatedBy: value.CreatedBy,
		CreatedAt: value.CreatedAt,
	}
}

type joinedServerResponse struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	CreatedBy int64  `json:"created_by"`
	CreatedAt int64  `json:"created_at"`
	Role      string `json:"role"`
	JoinedAt  int64  `json:"joined_at"`
}

func newJoinedServerResponse(
	value server.JoinedServer,
) joinedServerResponse {
	return joinedServerResponse{
		ID:        value.ID,
		Name:      value.Name,
		CreatedBy: value.CreatedBy,
		CreatedAt: value.CreatedAt,
		Role:      string(value.Role),
		JoinedAt:  value.JoinedAt,
	}
}

func newJoinedServersResponse(
	values []server.JoinedServer,
) []joinedServerResponse {
	response := make(
		[]joinedServerResponse,
		0,
		len(values),
	)

	for _, value := range values {
		response = append(
			response,
			newJoinedServerResponse(value),
		)
	}

	return response
}
