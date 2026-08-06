package serverhttp

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
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
}

type createRequest struct {
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
