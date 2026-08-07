package invitehttp

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"voxhold-backend/internal/account"
	"voxhold-backend/internal/httpapi"
	"voxhold-backend/internal/invite"
)

type Service interface {
	CreateDirect(
		ctx context.Context,
		serverID int64,
		inviterUserID int64,
		input invite.CreateDirectInput,
	) (invite.Invite, error)
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
		"POST /api/v1/servers/{serverID}/invites",
		requireAuth(http.HandlerFunc(h.createDirect)),
	)
}

type createDirectRequest struct {
	Username string `json:"username"`
}

func (h *Handler) createDirect(
	w http.ResponseWriter,
	r *http.Request,
) {
	inviterUserID, ok := account.UserIDFromContext(r.Context())
	if !ok {
		log.Print("create direct invite: user ID is missing from context")

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

	var request createDirectRequest

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

	createdInvite, err := h.service.CreateDirect(
		r.Context(),
		serverID,
		inviterUserID,
		invite.CreateDirectInput{
			Username: request.Username,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, invite.ErrUsernameInvalid),
			errors.Is(err, invite.ErrCannotInviteSelf):

			httpapi.WriteError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)

		case errors.Is(err, invite.ErrForbidden):
			httpapi.WriteError(
				w,
				http.StatusForbidden,
				"not allowed to invite users",
			)

		case errors.Is(err, invite.ErrUserNotFound):
			httpapi.WriteError(
				w,
				http.StatusNotFound,
				"user not found",
			)

		case errors.Is(err, invite.ErrAlreadyMember):
			httpapi.WriteError(
				w,
				http.StatusConflict,
				"user is already a server member",
			)

		case errors.Is(err, invite.ErrInviteAlreadyPending):
			httpapi.WriteError(
				w,
				http.StatusConflict,
				"invitation is already pending",
			)

		default:
			log.Printf("create direct invite: %v", err)

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
		newInviteResponse(createdInvite),
	)
}
