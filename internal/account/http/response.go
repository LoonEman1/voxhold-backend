package accounthttp

import (
	"encoding/json"
	"net/http"
	"voxhold-backend/internal/account"
)

type userResponse struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	CreatedAt int64  `json:"created_at"`
}

type sessionResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

type authResponse struct {
	User    userResponse    `json:"user"`
	Session sessionResponse `json:"session"`
}

func newAuthResponse(result account.LoginResult) authResponse {
	return authResponse{
		User: userResponse{
			ID:        result.User.ID,
			Username:  result.User.Username,
			CreatedAt: result.User.CreatedAt,
		},
		Session: sessionResponse{
			Token:     result.Session.Token,
			ExpiresAt: result.Session.ExpiresAt,
		},
	}
}

func writeJson(
	w http.ResponseWriter,
	status int,
	value any,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(value); err != nil {
		return
	}
}

func writeError(
	w http.ResponseWriter,
	status int,
	message string,
) {
	writeJson(w, status, map[string]string{
		"error": message,
	})
}
