package messagehttp

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"

	"voxhold-backend/internal/httpapi"
	"voxhold-backend/internal/message"
)

type interactionService interface {
	Forward(context.Context, int64, int64, int64, message.ForwardInput) (message.Message, error)
	SetReaction(context.Context, int64, int64, int64, int64, string, bool) (message.ReactionsData, error)
	ListReactors(context.Context, int64, int64, int64, int64, string, int64, int) (message.ReactorPage, error)
}

func interactionScope(w http.ResponseWriter, r *http.Request) (userID, serverID, channelID int64, ok bool) {
	if userID, ok = httpapi.AuthenticatedUserID(w, r); !ok {
		return
	}
	if serverID, ok = httpapi.PositiveInt64PathValue(w, r, "serverID", "server"); !ok {
		return
	}
	channelID, ok = httpapi.PositiveInt64PathValue(w, r, "channelID", "channel")
	w.Header().Set("Cache-Control", "no-store")
	return
}

func interactionError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	description := "internal server error"
	switch {
	case errors.Is(err, message.ErrForbidden):
		status = http.StatusForbidden
		description = "not allowed to access this channel"
	case errors.Is(err, message.ErrChannelNotFound), errors.Is(err, message.ErrMessageNotFound):
		status = http.StatusNotFound
		description = "message or channel not found"
	case errors.Is(err, message.ErrIdempotencyConflict):
		status = http.StatusConflict
		description = err.Error()
	case errors.Is(err, message.ErrReactionLimit):
		status = http.StatusConflict
		description = err.Error()
	case errors.Is(err, message.ErrReactionInvalid), errors.Is(err, message.ErrClientMessageIDInvalid), errors.Is(err, message.ErrReplyInvalid), errors.Is(err, message.ErrForwardInvalid):
		status = http.StatusBadRequest
		description = err.Error()
	default:
		log.Printf("message interaction: %v", err)
	}
	httpapi.WriteError(w, status, description)
}

func (h *Handler) forward(w http.ResponseWriter, r *http.Request) {
	userID, serverID, channelID, ok := interactionScope(w, r)
	if !ok {
		return
	}
	service, ok := h.service.(interactionService)
	if !ok {
		httpapi.WriteError(w, http.StatusNotImplemented, "message interactions unavailable")
		return
	}
	var input message.ForwardInput
	if !httpapi.DecodeJSON(w, r, &input) {
		return
	}
	value, err := service.Forward(r.Context(), serverID, channelID, userID, input)
	if err != nil {
		interactionError(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusCreated, newMessageResponse(value))
}

func (h *Handler) reactions(w http.ResponseWriter, r *http.Request) {
	userID, serverID, channelID, ok := interactionScope(w, r)
	if !ok {
		return
	}
	messageID, ok := httpapi.PositiveInt64PathValue(w, r, "messageID", "message")
	if !ok {
		return
	}
	service, ok := h.service.(interactionService)
	if !ok {
		httpapi.WriteError(w, http.StatusNotImplemented, "message interactions unavailable")
		return
	}
	emoji := r.PathValue("emoji")
	if r.Method == http.MethodGet {
		after := int64(0)
		if raw := r.URL.Query().Get("after_user_id"); raw != "" {
			var err error
			after, err = strconv.ParseInt(raw, 10, 64)
			if err != nil || after < 0 {
				httpapi.WriteError(w, http.StatusBadRequest, "invalid reactor cursor")
				return
			}
		}
		value, err := service.ListReactors(r.Context(), serverID, channelID, messageID, userID, emoji, after, 20)
		if err != nil {
			interactionError(w, err)
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, value)
		return
	}
	value, err := service.SetReaction(r.Context(), serverID, channelID, messageID, userID, emoji, r.Method == http.MethodPut)
	if err != nil {
		interactionError(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, value)
}
