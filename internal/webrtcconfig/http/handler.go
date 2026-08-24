// Package http exposes the runtime WebRTC ICE configuration to authorized
// browser clients so that TURN settings from the backend environment reach
// already built frontends without a frontend rebuild.
package http

import (
	"net/http"
	"slices"

	"voxhold-backend/internal/httpapi"
)

type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type configResponse struct {
	ICEServers        []ICEServer `json:"ice_servers"`
	ICETransportPolicy string     `json:"ice_transport_policy"`
}

// Handler serves the immutable, pre-validated ICE configuration. Credentials
// are never logged by this handler.
type Handler struct {
	urls       []string
	username   string
	credential string
}

func NewHandler(
	iceServerURLs []string,
	iceUsername string,
	iceCredential string,
) *Handler {
	return &Handler{
		// Keep an immutable copy so later mutations of the caller's slice can
		// never change what this endpoint returns.
		urls:       slices.Clone(iceServerURLs),
		username:   iceUsername,
		credential: iceCredential,
	}
}

type requireAuthFunc func(next http.Handler) http.Handler

func (h *Handler) RegisterRoutes(
	mux *http.ServeMux,
	requireAuth requireAuthFunc,
) {
	mux.Handle(
		"GET /api/v1/webrtc/config",
		requireAuth(http.HandlerFunc(h.serve)),
	)
}

func (h *Handler) serve(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	response := configResponse{
		ICETransportPolicy: "all",
	}
	if len(h.urls) > 0 {
		server := ICEServer{URLs: slices.Clone(h.urls)}
		if h.username != "" && h.credential != "" {
			server.Username = h.username
			server.Credential = h.credential
		}
		response.ICEServers = []ICEServer{server}
	} else {
		response.ICEServers = []ICEServer{}
	}

	httpapi.WriteJSON(w, http.StatusOK, response)
}
