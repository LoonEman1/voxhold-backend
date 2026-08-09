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

	Update(
		ctx context.Context,
		serverID int64,
		channelID int64,
		messageID int64,
		userID int64,
		input message.UpdateInput,
	) (message.Message, error)

	Delete(
		ctx context.Context,
		serverID int64,
		channelID int64,
		messageID int64,
		userID int64,
	) error

	Pin(
		ctx context.Context,
		serverID int64,
		channelID int64,
		messageID int64,
		userID int64,
	) error

	Unpin(
		ctx context.Context,
		serverID int64,
		channelID int64,
		messageID int64,
		userID int64,
	) error

	ListPinned(
		ctx context.Context,
		serverID int64,
		channelID int64,
		userID int64,
	) ([]message.PinnedMessage, error)
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

	mux.Handle(
		"PATCH /api/v1/servers/{serverID}/channels/{channelID}/messages/{messageID}",
		requireAuth(http.HandlerFunc(h.update)),
	)

	mux.Handle(
		"DELETE /api/v1/servers/{serverID}/channels/{channelID}/messages/{messageID}",
		requireAuth(http.HandlerFunc(h.deleteMessage)),
	)

	mux.Handle(
		"PUT /api/v1/servers/{serverID}/channels/{channelID}/messages/{messageID}/pin",
		requireAuth(http.HandlerFunc(h.pin)),
	)

	mux.Handle(
		"DELETE /api/v1/servers/{serverID}/channels/{channelID}/messages/{messageID}/pin",
		requireAuth(http.HandlerFunc(h.unpin)),
	)

	mux.Handle(
		"GET /api/v1/servers/{serverID}/channels/{channelID}/pins",
		requireAuth(http.HandlerFunc(h.listPinned)),
	)
}

type createRequest struct {
	Content string `json:"content"`
}

type updateRequest struct {
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

func (h *Handler) update(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	serverID, channelID, messageID, ok :=
		messagePathValues(w, r)
	if !ok {
		return
	}

	var request updateRequest
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

	updatedMessage, err := h.service.Update(
		r.Context(),
		serverID,
		channelID,
		messageID,
		userID,
		message.UpdateInput{Content: request.Content},
	)
	if err != nil {
		writeMessageOperationError(
			w, "update message", channelID, err,
		)
		return
	}

	httpapi.WriteJSON(
		w,
		http.StatusOK,
		newMessageResponse(updatedMessage),
	)
}

func (h *Handler) deleteMessage(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	serverID, channelID, messageID, ok :=
		messagePathValues(w, r)
	if !ok {
		return
	}

	if err := h.service.Delete(
		r.Context(),
		serverID,
		channelID,
		messageID,
		userID,
	); err != nil {
		writeMessageOperationError(
			w, "delete message", channelID, err,
		)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) pin(
	w http.ResponseWriter,
	r *http.Request,
) {
	h.changePin(w, r, true)
}

func (h *Handler) unpin(
	w http.ResponseWriter,
	r *http.Request,
) {
	h.changePin(w, r, false)
}

func (h *Handler) changePin(
	w http.ResponseWriter,
	r *http.Request,
	pinned bool,
) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	serverID, channelID, messageID, ok :=
		messagePathValues(w, r)
	if !ok {
		return
	}

	operation := "pin message"
	var err error

	if pinned {
		err = h.service.Pin(
			r.Context(),
			serverID,
			channelID,
			messageID,
			userID,
		)
	} else {
		operation = "unpin message"
		err = h.service.Unpin(
			r.Context(),
			serverID,
			channelID,
			messageID,
			userID,
		)
	}

	if err != nil {
		writeMessageOperationError(
			w, operation, channelID, err,
		)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listPinned(
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

	pinnedMessages, err := h.service.ListPinned(
		r.Context(),
		serverID,
		channelID,
		userID,
	)
	if err != nil {
		writeMessageOperationError(
			w, "list pinned messages", channelID, err,
		)
		return
	}

	httpapi.WriteJSON(
		w,
		http.StatusOK,
		newPinnedMessagesResponse(pinnedMessages),
	)
}

func messagePathValues(
	w http.ResponseWriter,
	r *http.Request,
) (int64, int64, int64, bool) {
	serverID, ok := httpapi.PositiveInt64PathValue(
		w, r, "serverID", "server",
	)
	if !ok {
		return 0, 0, 0, false
	}

	channelID, ok := httpapi.PositiveInt64PathValue(
		w, r, "channelID", "channel",
	)
	if !ok {
		return 0, 0, 0, false
	}

	messageID, ok := httpapi.PositiveInt64PathValue(
		w, r, "messageID", "message",
	)
	if !ok {
		return 0, 0, 0, false
	}

	return serverID, channelID, messageID, true
}

func writeMessageOperationError(
	w http.ResponseWriter,
	operation string,
	channelID int64,
	err error,
) {
	switch {
	case errors.Is(err, message.ErrContentRequired),
		errors.Is(err, message.ErrContentTooLong):

		httpapi.WriteError(w, http.StatusBadRequest, err.Error())

	case errors.Is(err, message.ErrForbidden),
		errors.Is(err, message.ErrEditForbidden),
		errors.Is(err, message.ErrDeleteForbidden),
		errors.Is(err, message.ErrPinForbidden):

		httpapi.WriteError(w, http.StatusForbidden, err.Error())

	case errors.Is(err, message.ErrMessageNotFound):
		httpapi.WriteError(
			w, http.StatusNotFound, "message not found",
		)

	case errors.Is(err, message.ErrChannelNotFound):
		httpapi.WriteError(
			w, http.StatusNotFound, "channel not found",
		)

	case errors.Is(err, message.ErrTextChannelRequired):
		httpapi.WriteError(
			w,
			http.StatusConflict,
			"messages are only available in text channels",
		)

	default:
		log.Printf("%s in channel %d: %v", operation, channelID, err)
		httpapi.WriteError(
			w,
			http.StatusInternalServerError,
			"internal server error",
		)
	}
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
