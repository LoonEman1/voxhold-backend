// Package http exposes the runtime WebRTC ICE configuration to authorized
// browser clients so that TURN settings from the backend environment reach
// already built frontends without a frontend rebuild.
package http

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"net/http"
	"slices"
	"strconv"
	"time"

	"voxhold-backend/internal/httpapi"
)

type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type configResponse struct {
	ICEServers         []ICEServer `json:"ice_servers"`
	ICETransportPolicy string      `json:"ice_transport_policy"`
	// CredentialExpiresAt is set only for time-limited TURN REST credentials.
	CredentialExpiresAt int64 `json:"credential_expires_at,omitempty"`
}

// Handler serves the immutable, pre-validated ICE configuration. Credentials
// are never logged by this handler.
type Handler struct {
	urls       []string
	username   string
	credential string

	turnSecret []byte
	turnTTL    time.Duration
	now        func() time.Time
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
		now:        time.Now,
	}
}

// WithTURNSecret switches to per-user TURN REST credentials (coturn
// --use-auth-secret). The secret never leaves the server.
func (h *Handler) WithTURNSecret(secret string, ttl time.Duration) *Handler {
	h.turnSecret = []byte(secret)
	h.turnTTL = ttl
	return h
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

// restCredential implements the TURN REST API: username "<expiry>:<user>",
// credential base64(HMAC-SHA1(secret, username)).
func restCredential(secret []byte, userID int64, expiresAt int64) (string, string) {
	username := strconv.FormatInt(expiresAt, 10) + ":" + strconv.FormatInt(userID, 10)
	mac := hmac.New(sha1.New, secret)
	mac.Write([]byte(username))
	return username, base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	response := configResponse{
		ICETransportPolicy: "all",
	}
	if len(h.urls) > 0 {
		server := ICEServer{URLs: slices.Clone(h.urls)}
		if len(h.turnSecret) > 0 {
			userID, ok := httpapi.AuthenticatedUserID(w, r)
			if !ok {
				return
			}
			expiresAt := h.now().Add(h.turnTTL).Unix()
			server.Username, server.Credential = restCredential(h.turnSecret, userID, expiresAt)
			response.CredentialExpiresAt = expiresAt
		} else if h.username != "" && h.credential != "" {
			server.Username = h.username
			server.Credential = h.credential
		}
		response.ICEServers = []ICEServer{server}
	} else {
		response.ICEServers = []ICEServer{}
	}

	httpapi.WriteJSON(w, http.StatusOK, response)
}
