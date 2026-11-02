package accounthttp

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"

	"voxhold-backend/internal/account"
	"voxhold-backend/internal/httpapi"
)

// SessionService is the optional self-service part of the account service.
type SessionService interface {
	ChangePassword(ctx context.Context, userID int64, currentToken string, input account.ChangePasswordInput) error
	ListSessions(ctx context.Context, userID int64, currentToken string) ([]account.SessionView, error)
	RevokeSessionByID(ctx context.Context, userID, sessionID int64) error
	RevokeOtherSessions(ctx context.Context, userID int64, currentToken string) error
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	NewConfirm      string `json:"new_password_confirm"`
}

// WithSessions enables password change and session management routes.
func (h *Handler) WithSessions(service SessionService) *Handler {
	h.sessions = service
	return h
}

// clientMetaContext records the user agent and address on sessions created by a request.
func clientMetaContext(r *http.Request, ip string) context.Context {
	return account.WithClientMeta(r.Context(), account.ClientMeta{UserAgent: r.UserAgent(), IP: ip})
}

func (h *Handler) registerSessionRoutes(mux *http.ServeMux) {
	mux.Handle("PUT /api/v1/me/password", h.RequireAuth(http.HandlerFunc(h.changePassword)))
	mux.Handle("GET /api/v1/me/sessions", h.RequireAuth(http.HandlerFunc(h.listSessions)))
	mux.Handle("DELETE /api/v1/me/sessions/{sessionID}", h.RequireAuth(http.HandlerFunc(h.deleteSession)))
	mux.Handle("DELETE /api/v1/me/sessions", h.RequireAuth(http.HandlerFunc(h.deleteOtherSessions)))
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	token, _ := bearerToken(r)
	var request changePasswordRequest
	if !httpapi.DecodeJSON(w, r, &request) {
		return
	}
	// A wrong current password counts against the same brute-force budget as login.
	identity := "password-change:" + strconv.FormatInt(userID, 10)
	if h.loginProtector != nil {
		if allowed, _ := h.loginProtector.AllowLogin(r, identity); !allowed {
			httpapi.WriteError(w, http.StatusTooManyRequests, "too many attempts; try again later")
			return
		}
	}
	err := h.sessions.ChangePassword(r.Context(), userID, token, account.ChangePasswordInput{
		CurrentPassword: request.CurrentPassword, NewPassword: request.NewPassword, NewConfirm: request.NewConfirm,
	})
	switch {
	case err == nil:
		if h.loginProtector != nil {
			h.loginProtector.RecordLoginSuccess(r, identity)
		}
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, account.ErrWrongCurrentPassword):
		if h.loginProtector != nil {
			h.loginProtector.RecordLoginFailure(r, identity)
		}
		httpapi.WriteError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, account.ErrPasswordTooShort), errors.Is(err, account.ErrPasswordTooLong), errors.Is(err, account.ErrPasswordsDoNotMatch):
		httpapi.WriteError(w, http.StatusBadRequest, err.Error())
	default:
		log.Printf("change password: %v", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	token, _ := bearerToken(r)
	views, err := h.sessions.ListSessions(r.Context(), userID, token)
	if err != nil {
		log.Printf("list sessions: %v", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"sessions": views})
}

func (h *Handler) deleteSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	sessionID, ok := httpapi.PositiveInt64PathValue(w, r, "sessionID", "session")
	if !ok {
		return
	}
	err := h.sessions.RevokeSessionByID(r.Context(), userID, sessionID)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, account.ErrSessionNotFound):
		httpapi.WriteError(w, http.StatusNotFound, "session not found")
	default:
		log.Printf("delete session: %v", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}

func (h *Handler) deleteOtherSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	token, _ := bearerToken(r)
	if err := h.sessions.RevokeOtherSessions(r.Context(), userID, token); err != nil {
		log.Printf("delete other sessions: %v", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
