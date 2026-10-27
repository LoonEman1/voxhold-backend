package notifications

import (
	"errors"
	"log"
	"net/http"
	"voxhold-backend/internal/httpapi"
)

func (s *Service) RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	for _, method := range []string{"GET", "PUT"} {
		mux.Handle(method+" /api/v1/servers/{serverID}/channels/{channelID}/notifications", auth(http.HandlerFunc(s.handle)))
	}
}
func (s *Service) handle(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	serverID, ok := httpapi.PositiveInt64PathValue(w, r, "serverID", "server")
	if !ok {
		return
	}
	channelID, ok := httpapi.PositiveInt64PathValue(w, r, "channelID", "channel")
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var value Settings
	var err error
	if r.Method == http.MethodPut {
		var input struct {
			Mode Mode `json:"mode"`
		}
		if !httpapi.DecodeJSON(w, r, &input) {
			return
		}
		value, err = s.Set(r.Context(), serverID, channelID, userID, input.Mode)
	} else {
		value, err = s.Get(r.Context(), serverID, channelID, userID)
	}
	if err != nil {
		status, description := http.StatusInternalServerError, "internal server error"
		switch {
		case errors.Is(err, ErrForbidden):
			status, description = http.StatusForbidden, ErrForbidden.Error()
		case errors.Is(err, ErrTextRequired), errors.Is(err, ErrMode):
			status, description = http.StatusBadRequest, err.Error()
		default:
			log.Printf("channel notification settings: %v", err)
		}
		httpapi.WriteError(w, status, description)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, value)
}
