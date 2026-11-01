package channelhttp

import (
	"context"
	"errors"
	"log"
	"net/http"

	"voxhold-backend/internal/channel"
	"voxhold-backend/internal/httpapi"
)

func (h *Handler) slowmode(w http.ResponseWriter, r *http.Request) {
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
	service, ok := h.service.(interface {
		GetSlowmode(context.Context, int64, int64, int64) (int, error)
		SetSlowmode(context.Context, int64, int64, int64, int) error
	})
	if !ok {
		httpapi.WriteError(w, http.StatusNotImplemented, "slowmode unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var seconds int
	var err error
	if r.Method == http.MethodPut {
		var input struct {
			Seconds *int `json:"seconds"`
		}
		if !httpapi.DecodeJSON(w, r, &input) {
			return
		}
		if input.Seconds == nil {
			httpapi.WriteError(w, http.StatusBadRequest, "seconds is required")
			return
		}
		seconds = *input.Seconds
		err = service.SetSlowmode(r.Context(), serverID, channelID, userID, seconds)
	} else {
		seconds, err = service.GetSlowmode(r.Context(), serverID, channelID, userID)
	}
	switch {
	case err == nil:
		httpapi.WriteJSON(w, http.StatusOK, map[string]int{"seconds": seconds})
	case errors.Is(err, channel.ErrSlowmodeInvalid):
		httpapi.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, channel.ErrForbidden):
		httpapi.WriteError(w, http.StatusForbidden, "only administrators can change slowmode")
	case errors.Is(err, channel.ErrNotFound):
		httpapi.WriteError(w, http.StatusNotFound, "channel not found")
	default:
		log.Printf("slowmode: %v", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}
