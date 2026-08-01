package accounthttp

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"voxhold-backend/internal/account"
)

type Service interface {
	Register(
		ctx context.Context,
		input account.RegisterInput,
	) (account.LoginResult, error)
	Login(
		ctx context.Context,
		input account.LoginInput,
	) (account.LoginResult, error)
}

type Handler struct {
	service Service
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
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var request registerRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&request); err != nil {
		writeError(
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

			writeError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)

		default:
			log.Printf("register user: %v", err)

			writeError(
				w,
				http.StatusInternalServerError,
				"internal server error",
			)
		}

		return
	}

	writeJson(
		w,
		http.StatusCreated,
		newAuthResponse(result),
	)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var request loginRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&request); err != nil {
		writeError(
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

			writeError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)

		case errors.Is(err, account.ErrInvalidCredentials):
			writeError(
				w,
				http.StatusUnauthorized,
				"invalid username or password",
			)

		default:
			log.Printf("login user: %v", err)

			writeError(
				w,
				http.StatusInternalServerError,
				"internal server error",
			)
		}

		return
	}

	writeJson(
		w,
		http.StatusOK,
		newAuthResponse(result),
	)

}
