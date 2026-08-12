package readstatehttp

import (
	"context"
	"errors"
	"log"
	"net/http"

	"voxhold-backend/internal/httpapi"
	"voxhold-backend/internal/readstate"
)

type Service interface {
	Mark(
		ctx context.Context,
		serverID int64,
		channelID int64,
		userID int64,
		input readstate.MarkInput,
	) (readstate.ChannelRead, error)
}

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(
	mux *http.ServeMux,
	requireAuth func(http.Handler) http.Handler,
) {
	mux.Handle(
		"PUT /api/v1/servers/{serverID}/channels/{channelID}/read",
		requireAuth(http.HandlerFunc(h.mark)),
	)
}

type markRequest struct {
	LastReadMessageID int64 `json:"last_read_message_id"`
}

func (h *Handler) mark(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	serverID, ok := httpapi.PositiveInt64PathValue(
		w, r, "serverID", "server",
	)
	if !ok {
		return
	}

	channelID, ok := httpapi.PositiveInt64PathValue(
		w, r, "channelID", "channel",
	)
	if !ok {
		return
	}

	var request markRequest
	if !httpapi.DecodeJSON(w, r, &request) {
		return
	}

	read, err := h.service.Mark(
		r.Context(),
		serverID,
		channelID,
		userID,
		readstate.MarkInput{
			LastReadMessageID: request.LastReadMessageID,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, readstate.ErrMessageIDInvalid):
			httpapi.WriteError(
				w, http.StatusBadRequest, err.Error(),
			)

		case errors.Is(err, readstate.ErrForbidden):
			httpapi.WriteError(
				w,
				http.StatusForbidden,
				"not allowed to mark this channel as read",
			)

		case errors.Is(err, readstate.ErrChannelNotFound):
			httpapi.WriteError(
				w, http.StatusNotFound, "channel not found",
			)

		case errors.Is(err, readstate.ErrMessageNotFound):
			httpapi.WriteError(
				w, http.StatusNotFound, "message not found",
			)

		case errors.Is(err, readstate.ErrTextChannelRequired):
			httpapi.WriteError(
				w,
				http.StatusConflict,
				err.Error(),
			)

		default:
			log.Printf(
				"mark channel %d read: %v",
				channelID,
				err,
			)
			httpapi.WriteError(
				w,
				http.StatusInternalServerError,
				"internal server error",
			)
		}

		return
	}

	httpapi.WriteJSON(
		w,
		http.StatusOK,
		newChannelReadResponse(read),
	)
}
