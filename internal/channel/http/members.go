package channelhttp

import (
	"errors"
	"log"
	"net/http"

	"voxhold-backend/internal/channel"
	"voxhold-backend/internal/httpapi"
)

func (h *Handler) listMembers(w http.ResponseWriter, r *http.Request) {
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
	ids, err := h.service.ListMemberIDs(r.Context(), serverID, channelID, userID)
	if err != nil {
		switch {
		case errors.Is(err, channel.ErrForbidden):
			httpapi.WriteError(w, http.StatusForbidden, "not allowed to view channel members")
		case errors.Is(err, channel.ErrNotFound):
			httpapi.WriteError(w, http.StatusNotFound, "channel not found")
		default:
			log.Printf("list channel members: %v", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpapi.WriteJSON(w, http.StatusOK, struct {
		UserIDs []int64 `json:"user_ids"`
	}{ids})
}
