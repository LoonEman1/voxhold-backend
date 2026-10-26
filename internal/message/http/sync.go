package messagehttp

import (
	"errors"
	"net/http"
	"strconv"

	"voxhold-backend/internal/httpapi"
	"voxhold-backend/internal/message"
)

func (h *Handler) sync(w http.ResponseWriter, r *http.Request) {
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
	var after *int64
	if raw := r.URL.Query().Get("after"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			httpapi.WriteError(w, 400, "invalid sync cursor")
			return
		}
		after = &value
	}
	page, err := h.service.Sync(r.Context(), serverID, channelID, userID, after)
	if err != nil {
		switch {
		case errors.Is(err, message.ErrForbidden):
			httpapi.WriteError(w, 403, "not allowed to access messages")
		case errors.Is(err, message.ErrChannelNotFound):
			httpapi.WriteError(w, 404, "channel not found")
		default:
			httpapi.WriteError(w, 500, "message sync failed")
		}
		return
	}
	values := make([]messageResponse, 0, len(page.Messages))
	for _, value := range page.Messages {
		values = append(values, newMessageResponse(value))
	}
	w.Header().Set("Cache-Control", "no-store")
	httpapi.WriteJSON(w, 200, struct {
		Messages   []messageResponse `json:"messages"`
		DeletedIDs []int64           `json:"deleted_ids"`
		Cursor     int64             `json:"cursor"`
		Reset      bool              `json:"reset"`
		HasMore    bool              `json:"has_more"`
		HasOlder   bool              `json:"has_older"`
	}{values, page.DeletedIDs, page.Cursor, page.Reset, page.HasMore, page.HasOlder})
}
