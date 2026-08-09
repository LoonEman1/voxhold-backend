package messagehttp

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"voxhold-backend/internal/httpapi"
	"voxhold-backend/internal/message"
)

type Service interface {
	Create(
		ctx context.Context,
		serverID int64,
		channelID int64,
		authorUserID int64,
		input message.CreateInput,
	) (message.Message, error)

	ListByChannelID(
		ctx context.Context,
		serverID int64,
		channelID int64,
		userID int64,
		input message.ListInput,
	) (message.Page, error)
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
		"POST /api/v1/servers/{serverID}/channels/{channelID}/messages",
		requireAuth(http.HandlerFunc(h.create)),
	)

	mux.Handle(
		"GET /api/v1/servers/{serverID}/channels/{channelID}/messages",
		requireAuth(http.HandlerFunc(h.listByChannelID)),
	)
}

type createRequest struct {
	Content string `json:"content"`
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

	channelID, ok := httpapi.PositiveInt64PathValue(
		w,
		r,
		"channelID",
		"channel",
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

	createdMessage, err := h.service.Create(
		r.Context(),
		serverID,
		channelID,
		userID,
		message.CreateInput{
			Content: request.Content,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, message.ErrContentRequired),
			errors.Is(err, message.ErrContentTooLong):

			httpapi.WriteError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)

		case errors.Is(err, message.ErrForbidden):
			httpapi.WriteError(
				w,
				http.StatusForbidden,
				"not allowed to send messages",
			)

		case errors.Is(err, message.ErrChannelNotFound):
			httpapi.WriteError(
				w,
				http.StatusNotFound,
				"channel not found",
			)

		case errors.Is(
			err,
			message.ErrTextChannelRequired,
		):
			httpapi.WriteError(
				w,
				http.StatusConflict,
				"messages can only be sent to text channels",
			)

		default:
			log.Printf(
				"create message in channel %d: %v",
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
		http.StatusCreated,
		newMessageResponse(createdMessage),
	)
}

func (h *Handler) listByChannelID(
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

	input, ok := messageListInput(w, r)
	if !ok {
		return
	}

	page, err := h.service.ListByChannelID(
		r.Context(),
		serverID,
		channelID,
		userID,
		input,
	)
	if err != nil {
		switch {
		case errors.Is(err, message.ErrPageLimitInvalid),
			errors.Is(err, message.ErrBeforeIDInvalid):

			httpapi.WriteError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)

		case errors.Is(err, message.ErrForbidden):
			httpapi.WriteError(
				w,
				http.StatusForbidden,
				"not allowed to view messages",
			)

		case errors.Is(err, message.ErrChannelNotFound):
			httpapi.WriteError(
				w,
				http.StatusNotFound,
				"channel not found",
			)

		case errors.Is(
			err,
			message.ErrTextChannelRequired,
		):
			httpapi.WriteError(
				w,
				http.StatusConflict,
				"messages are only available in text channels",
			)

		default:
			log.Printf(
				"list messages for channel %d: %v",
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
		newMessagePageResponse(page),
	)
}

func messageListInput(
	w http.ResponseWriter,
	r *http.Request,
) (message.ListInput, bool) {
	var input message.ListInput

	rawLimit := r.URL.Query().Get("limit")

	if rawLimit != "" {
		value, err := strconv.ParseInt(
			rawLimit,
			10,
			32,
		)
		if err != nil || value <= 0 {
			httpapi.WriteError(
				w,
				http.StatusBadRequest,
				"limit must be a positive integer",
			)
			return message.ListInput{}, false
		}

		input.Limit = int(value)
	}

	rawBeforeID := r.URL.Query().Get("before_id")

	if rawBeforeID != "" {
		value, err := strconv.ParseInt(
			rawBeforeID,
			10,
			64,
		)
		if err != nil || value <= 0 {
			httpapi.WriteError(
				w,
				http.StatusBadRequest,
				"before_id must be a positive integer",
			)
			return message.ListInput{}, false
		}

		input.BeforeID = &value
	}

	return input, true
}
