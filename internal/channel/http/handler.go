package channelhttp

import (
	"context"
	"encoding/json"
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
}

type createRequest struct {
	Name string       `json:"name"`
	Kind channel.Kind `json:"kind"`
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

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&request); err != nil {
		httpapi.WriteError(
			w,
			http.StatusBadRequest,
			"invalid JSON body",
		)
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
