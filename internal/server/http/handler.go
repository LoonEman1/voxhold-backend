package serverhttp

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"voxhold-backend/internal/account"
	"voxhold-backend/internal/httpapi"
	"voxhold-backend/internal/server"
)

type Service interface {
	Create(
		ctx context.Context,
		createdBy int64,
		input server.CreateInput,
	) (server.Server, error)

	Update(
		ctx context.Context,
		serverID int64,
		userID int64,
		input server.UpdateInput,
	) (server.Server, error)

	Delete(
		ctx context.Context,
		serverID int64,
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
		"POST /api/v1/servers",
		requireAuth(http.HandlerFunc(h.create)),
	)

	mux.Handle(
		"PATCH /api/v1/servers/{serverID}",
		requireAuth(http.HandlerFunc(h.update)),
	)

	mux.Handle(
		"DELETE /api/v1/servers/{serverID}",
		requireAuth(http.HandlerFunc(h.deleteServer)),
	)
}

type createRequest struct {
	Name string `json:"name"`
}

type updateRequest struct {
	Name string `json:"name"`
}

func (h *Handler) create(
	w http.ResponseWriter, r *http.Request,
) {
	userID, ok := account.UserIDFromContext(r.Context())
	if !ok {
		log.Print("create server: user ID is missing from context")

		httpapi.WriteError(
			w,
			http.StatusInternalServerError,
			"internal server error",
		)
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

	createdServer, err := h.service.Create(
		r.Context(),
		userID,
		server.CreateInput{
			Name: request.Name,
		},
	)

	if err != nil {
		switch {
		case errors.Is(err, server.ErrNameRequired),
			errors.Is(err, server.ErrNameTooLong):

			httpapi.WriteError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)

		case errors.Is(err, server.ErrAlreadyExists):
			httpapi.WriteError(
				w,
				http.StatusConflict,
				"server already exists",
			)

		default:
			log.Printf("create server: %v", err)

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
		newServerResponse(createdServer),
	)
}

func (h *Handler) update(
	w http.ResponseWriter, r *http.Request,
) {

	userID, ok := account.UserIDFromContext(r.Context())
	if !ok {
		log.Print("update server: user ID is missing from context")

		httpapi.WriteError(
			w,
			http.StatusInternalServerError,
			"internal server error",
		)
		return
	}

	serverID, err := strconv.ParseInt(
		r.PathValue("serverID"),
		10,
		64,
	)

	if err != nil || serverID <= 0 {
		httpapi.WriteError(
			w,
			http.StatusBadRequest,
			"invalid server ID",
		)
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
	updatedServer, err := h.service.Update(
		r.Context(),
		serverID,
		userID,
		server.UpdateInput{
			Name: request.Name,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, server.ErrNameRequired),
			errors.Is(err, server.ErrNameTooLong):

			httpapi.WriteError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)

		case errors.Is(err, server.ErrNotFound):
			httpapi.WriteError(
				w,
				http.StatusNotFound,
				"server not found",
			)

		default:
			log.Printf("update server: %v", err)

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
		newServerResponse(updatedServer),
	)
}

func (h *Handler) deleteServer(
	w http.ResponseWriter, r *http.Request,
) {
	userID, ok := account.UserIDFromContext(r.Context())

	if !ok {
		log.Print("delete server: user ID is missing from context")

		httpapi.WriteError(
			w,
			http.StatusInternalServerError,
			"internal server error",
		)
		return
	}

	serverID, err := strconv.ParseInt(
		r.PathValue("serverID"),
		10,
		64,
	)
	if err != nil || serverID <= 0 {
		httpapi.WriteError(
			w,
			http.StatusBadRequest,
			"invalid server ID",
		)
		return
	}

	err = h.service.Delete(r.Context(), serverID, userID)

	if err != nil {
		if errors.Is(err, server.ErrNotFound) {
			httpapi.WriteError(
				w,
				http.StatusNotFound,
				"server not found",
			)
			return
		}

		log.Printf("delete server: %v", err)

		httpapi.WriteError(
			w,
			http.StatusInternalServerError,
			"internal server error",
		)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
