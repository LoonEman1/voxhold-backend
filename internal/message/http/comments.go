package messagehttp

import (
	"context"
	"net/http"
	"voxhold-backend/internal/httpapi"
	"voxhold-backend/internal/message"
)

func (h *Handler) comments(w http.ResponseWriter, r *http.Request) {
	userID, serverID, channelID, ok := interactionScope(w, r)
	if !ok {
		return
	}
	postID, ok := httpapi.PositiveInt64PathValue(w, r, "messageID", "message")
	if !ok {
		return
	}
	input, ok := messageListInput(w, r)
	if !ok {
		return
	}
	service, ok := h.service.(interface {
		ListComments(context.Context, int64, int64, int64, int64, message.ListInput) (message.Page, error)
	})
	if !ok {
		httpapi.WriteError(w, http.StatusNotImplemented, "comments unavailable")
		return
	}
	page, err := service.ListComments(r.Context(), serverID, channelID, postID, userID, input)
	if err != nil {
		interactionError(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, newMessagePageResponse(page))
}
