package invitehttp

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

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

	ListIncoming(
		ctx context.Context,
		inviteeUserID int64,
	) ([]invite.IncomingInvite, error)

	Accept(
		ctx context.Context,
		inviteID int64,
		inviteeUserID int64,
	) error

	Decline(
		ctx context.Context,
		inviteID int64,
		inviteeUserID int64,
	) error

	CreateLink(
		ctx context.Context,
		serverID int64,
		creatorUserID int64,
		input invite.CreateLinkInput,
	) (invite.InviteLink, error)

	ResolveLink(
		ctx context.Context,
		token string,
	) (invite.LinkPreview, error)

	AcceptLink(
		ctx context.Context,
		token string,
		userID int64,
	) (int64, bool, error)
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

	mux.Handle(
		"POST /api/v1/servers/{serverID}/invite-links",
		requireAuth(http.HandlerFunc(h.createLink)),
	)

	mux.HandleFunc(
		"POST /api/v1/invite-links/resolve",
		h.resolveLink,
	)

	mux.Handle(
		"POST /api/v1/invite-links/accept",
		requireAuth(http.HandlerFunc(h.acceptLink)),
	)

	mux.Handle(
		"GET /api/v1/me/server-invites",
		requireAuth(http.HandlerFunc(h.listIncoming)),
	)

	mux.Handle(
		"POST /api/v1/me/server-invites/{inviteID}/accept",
		requireAuth(http.HandlerFunc(h.accept)),
	)

	mux.Handle(
		"POST /api/v1/me/server-invites/{inviteID}/decline",
		requireAuth(http.HandlerFunc(h.decline)),
	)
}

type createLinkRequest struct {
	ExpiresInSeconds  int64 `json:"expires_in_seconds"`
	MaxUses           *int  `json:"max_uses"`
	AllowRegistration bool  `json:"allow_registration"`
}

type linkTokenRequest struct {
	Token string `json:"token"`
}

type inviteLinkResponse struct {
	ID                int64  `json:"id"`
	ServerID          int64  `json:"server_id"`
	ServerName        string `json:"server_name"`
	CreatorUsername   string `json:"creator_username"`
	Token             string `json:"token"`
	ExpiresAt         int64  `json:"expires_at"`
	MaxUses           *int   `json:"max_uses"`
	UseCount          int    `json:"use_count"`
	AllowRegistration bool   `json:"allow_registration"`
	CreatedAt         int64  `json:"created_at"`
}

type linkPreviewResponse struct {
	ServerID          int64  `json:"server_id"`
	ServerName        string `json:"server_name"`
	CreatorUsername   string `json:"creator_username"`
	ExpiresAt         int64  `json:"expires_at"`
	MaxUses           *int   `json:"max_uses"`
	UseCount          int    `json:"use_count"`
	AllowRegistration bool   `json:"allow_registration"`
}

type linkAcceptanceResponse struct {
	ServerID      int64 `json:"server_id"`
	AlreadyMember bool  `json:"already_member"`
}

func (h *Handler) createLink(w http.ResponseWriter, r *http.Request) {
	creatorUserID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	serverID, ok := httpapi.PositiveInt64PathValue(w, r, "serverID", "server")
	if !ok {
		return
	}

	var request createLinkRequest
	if !httpapi.DecodeJSON(w, r, &request) {
		return
	}
	if request.ExpiresInSeconds < int64(invite.MinLinkLifetime/time.Second) ||
		request.ExpiresInSeconds > int64(invite.MaxLinkLifetime/time.Second) {
		httpapi.WriteError(w, http.StatusBadRequest, invite.ErrLinkLifetimeInvalid.Error())
		return
	}

	createdLink, err := h.service.CreateLink(
		r.Context(),
		serverID,
		creatorUserID,
		invite.CreateLinkInput{
			Lifetime:          time.Duration(request.ExpiresInSeconds) * time.Second,
			MaxUses:           request.MaxUses,
			AllowRegistration: request.AllowRegistration,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, invite.ErrLinkLifetimeInvalid),
			errors.Is(err, invite.ErrRegistrationLinkTooLong),
			errors.Is(err, invite.ErrLinkMaxUsesInvalid),
			errors.Is(err, invite.ErrRegistrationLimitNeeded):
			httpapi.WriteError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, invite.ErrForbidden):
			httpapi.WriteError(w, http.StatusForbidden, "not allowed to create invite links")
		default:
			log.Printf("create invite link: %v", err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	httpapi.WriteJSON(w, http.StatusCreated, inviteLinkResponse{
		ID:                createdLink.ID,
		ServerID:          createdLink.ServerID,
		ServerName:        createdLink.ServerName,
		CreatorUsername:   createdLink.CreatorUsername,
		Token:             createdLink.Token,
		ExpiresAt:         createdLink.ExpiresAt,
		MaxUses:           createdLink.MaxUses,
		UseCount:          createdLink.UseCount,
		AllowRegistration: createdLink.AllowRegistration,
		CreatedAt:         createdLink.CreatedAt,
	})
}

func (h *Handler) resolveLink(w http.ResponseWriter, r *http.Request) {
	var request linkTokenRequest
	if !httpapi.DecodeJSON(w, r, &request) {
		return
	}

	preview, err := h.service.ResolveLink(r.Context(), request.Token)
	if err != nil {
		if errors.Is(err, invite.ErrLinkInvalid) {
			httpapi.WriteError(w, http.StatusNotFound, "invite link is invalid or expired")
			return
		}

		log.Printf("resolve invite link: %v", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	httpapi.WriteJSON(w, http.StatusOK, linkPreviewResponse{
		ServerID:          preview.ServerID,
		ServerName:        preview.ServerName,
		CreatorUsername:   preview.CreatorUsername,
		ExpiresAt:         preview.ExpiresAt,
		MaxUses:           preview.MaxUses,
		UseCount:          preview.UseCount,
		AllowRegistration: preview.AllowRegistration,
	})
}

func (h *Handler) acceptLink(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	var request linkTokenRequest
	if !httpapi.DecodeJSON(w, r, &request) {
		return
	}

	serverID, alreadyMember, err := h.service.AcceptLink(
		r.Context(),
		request.Token,
		userID,
	)
	if err != nil {
		if errors.Is(err, invite.ErrLinkInvalid) {
			httpapi.WriteError(w, http.StatusNotFound, "invite link is invalid or expired")
			return
		}

		log.Printf("accept invite link: %v", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	httpapi.WriteJSON(w, http.StatusOK, linkAcceptanceResponse{
		ServerID:      serverID,
		AlreadyMember: alreadyMember,
	})
}

type createDirectRequest struct {
	Username string `json:"username"`
}

type incomingInviteResponse struct {
	ID              int64         `json:"id"`
	ServerID        int64         `json:"server_id"`
	ServerName      string        `json:"server_name"`
	InviterUserID   int64         `json:"inviter_user_id"`
	InviterUsername string        `json:"inviter_username"`
	Status          invite.Status `json:"status"`
	ExpiresAt       int64         `json:"expires_at"`
	CreatedAt       int64         `json:"created_at"`
}

func newIncomingInvitesResponse(
	values []invite.IncomingInvite,
) []incomingInviteResponse {
	response := make(
		[]incomingInviteResponse,
		0,
		len(values),
	)

	for _, value := range values {
		response = append(
			response,
			incomingInviteResponse{
				ID:              value.ID,
				ServerID:        value.ServerID,
				ServerName:      value.ServerName,
				InviterUserID:   value.InviterUserID,
				InviterUsername: value.InviterUsername,
				Status:          value.Status,
				ExpiresAt:       value.ExpiresAt,
				CreatedAt:       value.CreatedAt,
			},
		)
	}

	return response
}

func (h *Handler) createDirect(
	w http.ResponseWriter,
	r *http.Request,
) {
	inviterUserID, ok := httpapi.AuthenticatedUserID(w, r)
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

	var request createDirectRequest

	if !httpapi.DecodeJSON(w, r, &request) {
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

func (h *Handler) listIncoming(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	invitations, err := h.service.ListIncoming(
		r.Context(),
		userID,
	)
	if err != nil {
		log.Printf("list incoming invites: %v", err)

		httpapi.WriteError(
			w,
			http.StatusInternalServerError,
			"internal server error",
		)
		return
	}

	httpapi.WriteJSON(
		w,
		http.StatusOK,
		newIncomingInvitesResponse(invitations),
	)
}

func (h *Handler) accept(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	inviteID, ok := httpapi.PositiveInt64PathValue(
		w,
		r,
		"inviteID",
		"invitation",
	)
	if !ok {
		return
	}

	err := h.service.Accept(
		r.Context(),
		inviteID,
		userID,
	)
	if err != nil {
		writeRespondError(w, "accept invitation", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) decline(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	inviteID, ok := httpapi.PositiveInt64PathValue(
		w,
		r,
		"inviteID",
		"invitation",
	)
	if !ok {
		return
	}

	if err := h.service.Decline(
		r.Context(),
		inviteID,
		userID,
	); err != nil {
		writeRespondError(w, "decline invitation", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func writeRespondError(
	w http.ResponseWriter,
	operation string,
	err error,
) {
	switch {
	case errors.Is(err, invite.ErrInviteNotFound):
		httpapi.WriteError(
			w,
			http.StatusNotFound,
			"invitation not found",
		)

	case errors.Is(err, invite.ErrInviteNotPending):
		httpapi.WriteError(
			w,
			http.StatusConflict,
			"invitation is no longer pending",
		)

	case errors.Is(err, invite.ErrInviteExpired):
		httpapi.WriteError(
			w,
			http.StatusConflict,
			"invitation has expired",
		)

	default:
		log.Printf("%s: %v", operation, err)

		httpapi.WriteError(
			w,
			http.StatusInternalServerError,
			"internal server error",
		)
	}
}
