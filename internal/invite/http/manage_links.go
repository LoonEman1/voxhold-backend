package invitehttp

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"voxhold-backend/internal/httpapi"
	"voxhold-backend/internal/invite"
)

// Management lists expose metadata only. Original bearer tokens are shown once
// at creation and are stored exclusively as hashes in the database.
type managedLinkResponse struct {
	ID                int64  `json:"id"`
	ServerID          int64  `json:"server_id"`
	CreatedBy         int64  `json:"created_by"`
	CreatorUsername   string `json:"creator_username"`
	ExpiresAt         int64  `json:"expires_at"`
	MaxUses           *int   `json:"max_uses"`
	UseCount          int    `json:"use_count"`
	AllowRegistration bool   `json:"allow_registration"`
	CreatedAt         int64  `json:"created_at"`
}

type managedLinkPagination struct {
	NextBeforeID *int64 `json:"next_before_id"`
	HasMore      bool   `json:"has_more"`
}

func (h *Handler) linkManagementError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, invite.ErrForbidden):
		httpapi.WriteError(w, 403, "not allowed to manage invite links")
	case errors.Is(err, invite.ErrLinkNotFound):
		httpapi.WriteError(w, 404, "invite link not found")
	case errors.Is(err, invite.ErrLinkPaginationInvalid):
		httpapi.WriteError(w, 400, invite.ErrLinkPaginationInvalid.Error())
	default:
		log.Printf("manage invite links: %v", err)
		httpapi.WriteError(w, 500, "internal server error")
	}
}

func (h *Handler) listLinks(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	serverID, ok := httpapi.PositiveInt64PathValue(w, r, "serverID", "server")
	if !ok {
		return
	}
	input := invite.ListLinksInput{}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			h.linkManagementError(w, invite.ErrLinkPaginationInvalid)
			return
		}
		input.Limit = value
	}
	if raw := r.URL.Query().Get("before_id"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			h.linkManagementError(w, invite.ErrLinkPaginationInvalid)
			return
		}
		input.BeforeID = &value
	}
	page, err := h.service.ListActiveLinks(r.Context(), serverID, userID, input)
	if err != nil {
		h.linkManagementError(w, err)
		return
	}
	values := make([]managedLinkResponse, 0, len(page.Links))
	for _, link := range page.Links {
		values = append(values, managedLinkResponse{link.ID, link.ServerID, link.CreatedBy, link.CreatorUsername, link.ExpiresAt, link.MaxUses, link.UseCount, link.AllowRegistration, link.CreatedAt})
	}
	w.Header().Set("Cache-Control", "no-store")
	httpapi.WriteJSON(w, 200, struct {
		Links      []managedLinkResponse `json:"invite_links"`
		Pagination managedLinkPagination `json:"pagination"`
	}{Links: values, Pagination: managedLinkPagination{page.NextBeforeID, page.HasMore}})
}

func (h *Handler) revokeLink(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	serverID, ok := httpapi.PositiveInt64PathValue(w, r, "serverID", "server")
	if !ok {
		return
	}
	linkID, ok := httpapi.PositiveInt64PathValue(w, r, "linkID", "invite link")
	if !ok {
		return
	}
	if err := h.service.RevokeLink(r.Context(), serverID, linkID, userID); err != nil {
		h.linkManagementError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
