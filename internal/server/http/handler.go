package serverhttp

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

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

	Leave(
		ctx context.Context,
		serverID int64,
		userID int64,
	) error

	ListByUserID(
		ctx context.Context,
		userID int64,
	) ([]server.JoinedServer, error)

	ListMembers(
		ctx context.Context,
		serverID int64,
		requesterUserID int64,
	) ([]server.ServerMember, error)

	UpdateMemberRole(
		ctx context.Context,
		serverID int64,
		requesterUserID int64,
		targetUserID int64,
		input server.UpdateMemberRoleInput,
	) (server.ServerMember, error)

	KickMember(
		ctx context.Context,
		serverID int64,
		requesterUserID int64,
		targetUserID int64,
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

	mux.Handle(
		"DELETE /api/v1/servers/{serverID}/members/me",
		requireAuth(http.HandlerFunc(h.leave)),
	)

	mux.Handle(
		"GET /api/v1/me/servers",
		requireAuth(http.HandlerFunc(h.listForCurrentUser)),
	)

	mux.Handle(
		"GET /api/v1/servers/{serverID}/members",
		requireAuth(http.HandlerFunc(h.listMembers)),
	)

	mux.Handle(
		"PATCH /api/v1/servers/{serverID}/members/{userID}/role",
		requireAuth(http.HandlerFunc(h.updateMemberRole)),
	)

	mux.Handle(
		"DELETE /api/v1/servers/{serverID}/members/{userID}",
		requireAuth(http.HandlerFunc(h.kickMember)),
	)
}

type createRequest struct {
	Name string `json:"name"`
}

type updateRequest struct {
	Name string `json:"name"`
}

type updateMemberRoleRequest struct {
	Role server.Role `json:"role"`
}

func (h *Handler) create(
	w http.ResponseWriter, r *http.Request,
) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
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

	err := h.service.Delete(r.Context(), serverID, userID)

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

func (h *Handler) leave(
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

	if err := h.service.Leave(
		r.Context(),
		serverID,
		userID,
	); err != nil {
		switch {
		case errors.Is(err, server.ErrMembershipNotFound):
			httpapi.WriteError(
				w,
				http.StatusNotFound,
				"server membership not found",
			)

		case errors.Is(err, server.ErrOwnerCannotLeave):
			httpapi.WriteError(
				w,
				http.StatusConflict,
				"server owner cannot leave the server",
			)

		default:
			log.Printf("leave server: %v", err)

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

func (h *Handler) listForCurrentUser(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	joinedServers, err := h.service.ListByUserID(
		r.Context(),
		userID,
	)
	if err != nil {
		log.Printf(
			"list servers for user %d: %v",
			userID,
			err,
		)

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
		newJoinedServersResponse(joinedServers),
	)
}

func (h *Handler) listMembers(
	w http.ResponseWriter,
	r *http.Request,
) {
	requesterUserID, ok := httpapi.AuthenticatedUserID(w, r)
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

	members, err := h.service.ListMembers(
		r.Context(),
		serverID,
		requesterUserID,
	)
	if err != nil {
		switch {
		case errors.Is(err, server.ErrMembersForbidden):
			httpapi.WriteError(
				w,
				http.StatusForbidden,
				"not allowed to view server members",
			)

		default:
			log.Printf(
				"list members for server %d: %v",
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
		newServerMembersResponse(members),
	)
}

func (h *Handler) updateMemberRole(
	w http.ResponseWriter,
	r *http.Request,
) {
	requesterUserID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	serverID, ok := httpapi.PositiveInt64PathValue(
		w, r, "serverID", "server",
	)
	if !ok {
		return
	}

	targetUserID, ok := httpapi.PositiveInt64PathValue(
		w, r, "userID", "user",
	)
	if !ok {
		return
	}

	var request updateMemberRoleRequest
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

	member, err := h.service.UpdateMemberRole(
		r.Context(),
		serverID,
		requesterUserID,
		targetUserID,
		server.UpdateMemberRoleInput{Role: request.Role},
	)
	if err != nil {
		writeManageMemberError(
			w, "update member role", serverID, err,
		)
		return
	}

	httpapi.WriteJSON(
		w,
		http.StatusOK,
		newServerMemberResponse(member),
	)
}

func (h *Handler) kickMember(
	w http.ResponseWriter,
	r *http.Request,
) {
	requesterUserID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	serverID, ok := httpapi.PositiveInt64PathValue(
		w, r, "serverID", "server",
	)
	if !ok {
		return
	}

	targetUserID, ok := httpapi.PositiveInt64PathValue(
		w, r, "userID", "user",
	)
	if !ok {
		return
	}

	if err := h.service.KickMember(
		r.Context(),
		serverID,
		requesterUserID,
		targetUserID,
	); err != nil {
		writeManageMemberError(
			w, "kick member", serverID, err,
		)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func writeManageMemberError(
	w http.ResponseWriter,
	operation string,
	serverID int64,
	err error,
) {
	switch {
	case errors.Is(err, server.ErrRoleInvalid):
		httpapi.WriteError(
			w, http.StatusBadRequest, err.Error(),
		)

	case errors.Is(err, server.ErrMemberNotFound):
		httpapi.WriteError(
			w, http.StatusNotFound, "server member not found",
		)

	case errors.Is(err, server.ErrManageMembersForbidden):
		httpapi.WriteError(
			w, http.StatusForbidden, err.Error(),
		)

	case errors.Is(err, server.ErrOwnerRoleImmutable),
		errors.Is(err, server.ErrCannotChangeOwnRole),
		errors.Is(err, server.ErrOwnerCannotBeKicked),
		errors.Is(err, server.ErrCannotKickSelf):

		httpapi.WriteError(
			w, http.StatusConflict, err.Error(),
		)

	default:
		log.Printf(
			"%s in server %d: %v",
			operation,
			serverID,
			err,
		)
		httpapi.WriteError(
			w,
			http.StatusInternalServerError,
			"internal server error",
		)
	}
}
