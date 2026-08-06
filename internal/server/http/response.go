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
