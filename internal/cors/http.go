package cors

import (
	"errors"
	"log"
	"net/http"

	"voxhold-backend/internal/httpapi"
)

type originsRequest struct {
	Origins []string `json:"origins"`
}

type originsResponse struct {
	Origins []string `json:"origins"`
}

func (m *Manager) RegisterRoutes(
	mux *http.ServeMux,
	requireAuth func(http.Handler) http.Handler,
) {
	mux.Handle(
		"GET /api/v1/instance/cors-origins",
		requireAuth(http.HandlerFunc(m.listOrigins)),
	)
	mux.Handle(
		"PUT /api/v1/instance/cors-origins",
		requireAuth(http.HandlerFunc(m.replaceOrigins)),
	)
}

func (m *Manager) listOrigins(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	origins, err := m.List(r.Context(), userID)
	if err != nil {
		m.writeSettingsError(w, "list CORS origins", err)
		return
	}

	httpapi.WriteJSON(w, http.StatusOK, originsResponse{Origins: origins})
}

func (m *Manager) replaceOrigins(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	var request originsRequest
	if !httpapi.DecodeJSON(w, r, &request) {
		return
	}
	if request.Origins == nil {
		request.Origins = []string{}
	}

	origins, err := m.Replace(r.Context(), userID, request.Origins)
	if err != nil {
		m.writeSettingsError(w, "replace CORS origins", err)
		return
	}

	httpapi.WriteJSON(w, http.StatusOK, originsResponse{Origins: origins})
}

func (m *Manager) writeSettingsError(
	w http.ResponseWriter,
	operation string,
	err error,
) {
	switch {
	case errors.Is(err, ErrForbidden):
		httpapi.WriteError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrInvalidOrigin), errors.Is(err, ErrTooManyOrigins):
		httpapi.WriteError(w, http.StatusBadRequest, err.Error())
	default:
		log.Printf("%s: %v", operation, err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}
