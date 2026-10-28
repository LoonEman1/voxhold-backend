package channelhttp

import (
	"context"
	"errors"
	"log"
	"net/http"
	"voxhold-backend/internal/channel"
	"voxhold-backend/internal/httpapi"
)

func (h *Handler) news(w http.ResponseWriter, r *http.Request) {
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
	service, ok := h.service.(interface {
		GetNews(context.Context, int64, int64, int64) (channel.NewsSettings, error)
		SetNews(context.Context, int64, int64, int64, channel.NewsSettings) (channel.NewsSettings, error)
	})
	if !ok {
		httpapi.WriteError(w, http.StatusNotImplemented, "news settings unavailable")
		return
	}
	var value channel.NewsSettings
	var err error
	if r.Method == http.MethodPut {
		var input struct {
			PublisherIDs    []int64 `json:"publisher_ids"`
			CommentsEnabled *bool   `json:"comments_enabled"`
		}
		if !httpapi.DecodeJSON(w, r, &input) {
			return
		}
		if input.CommentsEnabled == nil {
			httpapi.WriteError(w, http.StatusBadRequest, "comments_enabled is required")
			return
		}
		value, err = service.SetNews(r.Context(), serverID, channelID, userID, channel.NewsSettings{PublisherIDs: input.PublisherIDs, CommentsEnabled: *input.CommentsEnabled})
	} else {
		value, err = service.GetNews(r.Context(), serverID, channelID, userID)
	}
	if err != nil {
		switch {
		case errors.Is(err, channel.ErrForbidden):
			httpapi.WriteError(w, http.StatusForbidden, "only administrators can manage news settings")
		case errors.Is(err, channel.ErrNotFound):
			httpapi.WriteError(w, http.StatusNotFound, "channel not found")
		case errors.Is(err, channel.ErrAccessInvalid):
			httpapi.WriteError(w, http.StatusBadRequest, "choose a news channel and current server members")
		default:
			log.Printf("news settings: %v", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, value)
}
