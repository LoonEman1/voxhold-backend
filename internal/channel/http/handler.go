package channelhttp

import (
	"context"
	"errors"
	"log"
	"net/http"

	"voxhold-backend/internal/channel"
	"voxhold-backend/internal/httpapi"
)

type Service interface {
	Create(
		ctx context.Context,
		serverID int64,
		userID int64,
		input channel.CreateInput,
	) (channel.Channel, error)

	ListByServerID(
		ctx context.Context,
		serverID int64,
		userID int64,
	) ([]channel.Channel, error)

	Update(
		ctx context.Context,
		serverID int64,
		channelID int64,
		userID int64,
		input channel.UpdateInput,
	) (channel.Channel, error)

	Delete(
		ctx context.Context,
		serverID int64,
		channelID int64,
		userID int64,
	) error
}

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{
		service: service,
	}
}

func (h *Handler) RegisterRoutes(
	mux *http.ServeMux,
	requireAuth func(http.Handler) http.Handler,
) {
	mux.Handle(
		"POST /api/v1/servers/{serverID}/channels",
		requireAuth(http.HandlerFunc(h.create)),
	)

	mux.Handle(
		"GET /api/v1/servers/{serverID}/channels",
		requireAuth(http.HandlerFunc(h.listByServerID)),
	)

	mux.Handle(
		"PATCH /api/v1/servers/{serverID}/channels/{channelID}",
		requireAuth(http.HandlerFunc(h.update)),
	)

	mux.Handle(
		"DELETE /api/v1/servers/{serverID}/channels/{channelID}",
		requireAuth(http.HandlerFunc(h.deleteChannel)),
	)
}

type createRequest struct {
	Name string       `json:"name"`
	Kind channel.Kind `json:"kind"`
}

type updateRequest struct {
	Name string `json:"name"`
}

func (h *Handler) create(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	serverID, ok := httpapi.PositiveInt64PathValue(
		w,
		r,
		"serverID",
		"server",
	)
	if !ok {
		return
	}

	var request createRequest

	if !httpapi.DecodeJSON(w, r, &request) {
		return
	}

	createdChannel, err := h.service.Create(
		r.Context(),
		serverID,
		userID,
		channel.CreateInput{
			Name: request.Name,
			Kind: request.Kind,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, channel.ErrNameRequired),
			errors.Is(err, channel.ErrNameTooLong),
			errors.Is(err, channel.ErrKindInvalid):

			httpapi.WriteError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)

		case errors.Is(err, channel.ErrForbidden):
			httpapi.WriteError(
				w,
				http.StatusForbidden,
				"not allowed to create channel",
			)

		case errors.Is(err, channel.ErrNameAlreadyExists):
			httpapi.WriteError(
				w,
				http.StatusConflict,
				"channel name already exists",
			)

		default:
			log.Printf("create channel: %v", err)

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
		http.StatusCreated,
		newChannelResponse(createdChannel),
	)
}

func (h *Handler) listByServerID(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	serverID, ok := httpapi.PositiveInt64PathValue(
		w,
		r,
		"serverID",
		"server",
	)
	if !ok {
		return
	}

	channels, err := h.service.ListByServerID(
		r.Context(),
		serverID,
		userID,
	)
	if err != nil {
		switch {
		case errors.Is(err, channel.ErrForbidden):
			httpapi.WriteError(
				w,
				http.StatusForbidden,
				"not allowed to view channels",
			)

		default:
			log.Printf(
				"list channels for server %d: %v",
				serverID,
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
		newChannelsResponse(channels),
	)
}

func (h *Handler) update(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	serverID, ok := httpapi.PositiveInt64PathValue(
		w,
		r,
		"serverID",
		"server",
	)
	if !ok {
		return
	}

	channelID, ok := httpapi.PositiveInt64PathValue(
		w,
		r,
		"channelID",
		"channel",
	)
	if !ok {
		return
	}

	var request updateRequest

	if !httpapi.DecodeJSON(w, r, &request) {
		return
	}

	updatedChannel, err := h.service.Update(
		r.Context(),
		serverID,
		channelID,
		userID,
		channel.UpdateInput{
			Name: request.Name,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, channel.ErrNameRequired),
			errors.Is(err, channel.ErrNameTooLong):

			httpapi.WriteError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)

		case errors.Is(err, channel.ErrNotFound):
			httpapi.WriteError(
				w,
				http.StatusNotFound,
				"channel not found",
			)

		case errors.Is(err, channel.ErrForbidden):
			httpapi.WriteError(
				w,
				http.StatusForbidden,
				"not allowed to update channel",
			)

		case errors.Is(err, channel.ErrNameAlreadyExists):
			httpapi.WriteError(
				w,
				http.StatusConflict,
				"channel name already exists",
			)

		default:
			log.Printf(
				"update channel %d: %v",
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
		newChannelResponse(updatedChannel),
	)
}

func (h *Handler) deleteChannel(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	serverID, ok := httpapi.PositiveInt64PathValue(
		w,
		r,
		"serverID",
		"server",
	)
	if !ok {
		return
	}

	channelID, ok := httpapi.PositiveInt64PathValue(
		w,
		r,
		"channelID",
		"channel",
	)
	if !ok {
		return
	}

	err := h.service.Delete(
		r.Context(),
		serverID,
		channelID,
		userID,
	)
	if err != nil {
		switch {
		case errors.Is(err, channel.ErrNotFound):
			httpapi.WriteError(
				w,
				http.StatusNotFound,
				"channel not found",
			)

		case errors.Is(err, channel.ErrForbidden):
			httpapi.WriteError(
				w,
				http.StatusForbidden,
				"not allowed to delete channel",
			)

		default:
			log.Printf(
				"delete channel %d: %v",
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

	w.WriteHeader(http.StatusNoContent)
}
