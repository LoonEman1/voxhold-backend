package accounthttp

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"voxhold-backend/internal/account"
	"voxhold-backend/internal/httpapi"
)

func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	parts := strings.Fields(header)

	if len(parts) != 2 {
		return "", false
	}

	if !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}

	if parts[1] == "" {
		return "", false
	}

	return parts[1], true
}

type Service interface {
	Register(
		ctx context.Context,
		input account.RegisterInput,
	) (account.LoginResult, error)
	Login(
		ctx context.Context,
		input account.LoginInput,
	) (account.LoginResult, error)
	Logout(
		ctx context.Context,
		token string,
	) error

	Authenticate(
		ctx context.Context, token string,
	) (int64, error)
}

type registerRequest struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	PasswordConfirm string `json:"password_confirm"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc(
		"POST /api/v1/auth/register",
		h.register,
	)

	mux.HandleFunc(
		"POST /api/v1/auth/login",
		h.login,
	)

	mux.HandleFunc(
		"POST /api/v1/auth/logout",
		h.logout,
	)
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var request registerRequest

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

	result, err := h.service.Register(
		r.Context(),
		account.RegisterInput{
			Username:        request.Username,
			Password:        request.Password,
			PasswordConfirm: request.PasswordConfirm,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, account.ErrUsernameInvalid),
			errors.Is(err, account.ErrPasswordTooShort),
			errors.Is(err, account.ErrPasswordTooLong),
			errors.Is(err, account.ErrPasswordsDoNotMatch):

			httpapi.WriteError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)

		default:
			log.Printf("register user: %v", err)

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
		newAuthResponse(result),
	)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		httpapi.WriteError(
			w,
			http.StatusUnauthorized,
			"authorization token is required",
		)
		return
	}

	if err := h.service.Logout(r.Context(), token); err != nil {
		log.Printf("logout: %v", err)

		httpapi.WriteError(
			w,
			http.StatusInternalServerError,
			"internal server error",
		)
		return
	}

	w.WriteHeader(http.StatusNoContent)

}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var request loginRequest

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

	result, err := h.service.Login(
		r.Context(),
		account.LoginInput{
			Username: request.Username,
			Password: request.Password,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, account.ErrLoginUsernameRequired),
			errors.Is(err, account.ErrLoginPasswordRequired):

			httpapi.WriteError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)

		case errors.Is(err, account.ErrInvalidCredentials):
			httpapi.WriteError(
				w,
				http.StatusUnauthorized,
				"invalid username or password",
			)

		default:
			log.Printf("login user: %v", err)

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
		newAuthResponse(result),
	)
}
