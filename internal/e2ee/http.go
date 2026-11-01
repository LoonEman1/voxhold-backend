package e2ee

import (
	"errors"
	"log"
	"net/http"

	"voxhold-backend/internal/httpapi"
)

// Handler serves device keys and the per-channel encryption policy.
type Handler struct {
	repository *Repository
	// onPolicyChanged lets the realtime layer apply a new policy to live voice
	// sessions: everyone in the channel reconnects in the new mode.
	onPolicyChanged func(serverID, channelID int64, required bool)
}

func NewHandler(repository *Repository, onPolicyChanged func(serverID, channelID int64, required bool)) *Handler {
	return &Handler{repository: repository, onPolicyChanged: onPolicyChanged}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, requireAuth func(http.Handler) http.Handler) {
	mux.Handle("PUT /api/v1/me/e2ee/devices/{deviceID}", requireAuth(http.HandlerFunc(h.register)))
	mux.Handle("DELETE /api/v1/me/e2ee/devices/{deviceID}", requireAuth(http.HandlerFunc(h.revoke)))
	mux.Handle("GET /api/v1/users/{userID}/e2ee/devices", requireAuth(http.HandlerFunc(h.devices)))
	mux.Handle("GET /api/v1/servers/{serverID}/channels/{channelID}/e2ee", requireAuth(http.HandlerFunc(h.policy)))
	mux.Handle("PUT /api/v1/servers/{serverID}/channels/{channelID}/e2ee", requireAuth(http.HandlerFunc(h.policy)))
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	var input struct {
		// encoding/json decodes base64 into []byte.
		PublicKey []byte `json:"public_key"`
	}
	if !httpapi.DecodeJSON(w, r, &input) {
		return
	}
	err := h.repository.Register(r.Context(), userID, r.PathValue("deviceID"), input.PublicKey)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ErrInvalidDevice), errors.Is(err, ErrInvalidPublicKey):
		httpapi.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrKeyConflict):
		httpapi.WriteError(w, http.StatusConflict, err.Error())
	default:
		log.Printf("register e2ee device: %v", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	err := h.repository.Revoke(r.Context(), userID, r.PathValue("deviceID"))
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ErrInvalidDevice):
		httpapi.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		httpapi.WriteError(w, http.StatusNotFound, "device not found")
	default:
		log.Printf("revoke e2ee device: %v", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}

func (h *Handler) devices(w http.ResponseWriter, r *http.Request) {
	viewerID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	targetID, ok := httpapi.PositiveInt64PathValue(w, r, "userID", "user")
	if !ok {
		return
	}
	devices, err := h.repository.Devices(r.Context(), viewerID, targetID)
	switch {
	case err == nil:
		w.Header().Set("Cache-Control", "no-store")
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{"devices": devices})
	case errors.Is(err, ErrNotFound):
		httpapi.WriteError(w, http.StatusNotFound, "user not found")
	default:
		log.Printf("list e2ee devices: %v", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}

func (h *Handler) policy(w http.ResponseWriter, r *http.Request) {
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
	var required bool
	var err error
	if r.Method == http.MethodPut {
		var input struct {
			Required *bool `json:"required"`
		}
		if !httpapi.DecodeJSON(w, r, &input) {
			return
		}
		if input.Required == nil {
			httpapi.WriteError(w, http.StatusBadRequest, "required is required")
			return
		}
		required = *input.Required
		var changed bool
		changed, err = h.repository.SetChannelPolicy(r.Context(), serverID, channelID, userID, required)
		if err == nil && changed && h.onPolicyChanged != nil {
			h.onPolicyChanged(serverID, channelID, required)
		}
	} else {
		required, err = h.repository.ChannelPolicy(r.Context(), serverID, channelID, userID)
	}
	switch {
	case err == nil:
		httpapi.WriteJSON(w, http.StatusOK, map[string]bool{"required": required})
	case errors.Is(err, ErrForbidden):
		httpapi.WriteError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrNotFound):
		httpapi.WriteError(w, http.StatusNotFound, "voice channel not found")
	default:
		log.Printf("e2ee channel policy: %v", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}
