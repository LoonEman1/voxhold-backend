package channelhttp

import (
	"context"
	"errors"
	"log"
	"net/http"

	"voxhold-backend/internal/channel"
	"voxhold-backend/internal/httpapi"
	"voxhold-backend/internal/server"
)

type accessService interface {
	GetAccess(context.Context, int64, int64, int64) (channel.Access, error)
	SetAccess(context.Context, int64, int64, int64, channel.Access) (channel.Access, error)
}

func (h *Handler) access(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	serverID, ok := httpapi.PositiveInt64PathValue(w, r, "serverID", "server")
	if !ok {
		return
	}
	channelID, ok := httpapi.PositiveInt64PathValue(w, r, "channelID", "channel")
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	service := h.service
	var value channel.Access
	var err error
	if r.Method == http.MethodPut {
		var input struct {
			Restricted *bool         `json:"restricted"`
			UserIDs    []int64       `json:"user_ids"`
			Roles      []server.Role `json:"roles"`
		}
		if !httpapi.DecodeJSON(w, r, &input) {
			return
		}
		if input.Restricted == nil {
			httpapi.WriteError(w, http.StatusBadRequest, "restricted is required")
			return
		}
		value, err = service.SetAccess(r.Context(), serverID, channelID, userID, channel.Access{Restricted: *input.Restricted, UserIDs: input.UserIDs, Roles: input.Roles})
	} else {
		value, err = service.GetAccess(r.Context(), serverID, channelID, userID)
	}
	if err != nil {
		status, message := http.StatusInternalServerError, "internal server error"
		switch {
		case errors.Is(err, channel.ErrForbidden):
			status, message = http.StatusForbidden, "only server administrators can manage channel access"
		case errors.Is(err, channel.ErrNotFound):
			status, message = http.StatusNotFound, "channel not found"
		case errors.Is(err, channel.ErrAccessInvalid):
			status, message = http.StatusBadRequest, "invalid roles or users: choose current server members"
		default:
			log.Printf("channel access: %v", err)
		}
		httpapi.WriteError(w, status, message)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, value)
}
