package accounthttp

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
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

	Refresh(
		ctx context.Context,
		currentToken string,
	) (account.SessionInfo, error)
}

type registerRequest struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	PasswordConfirm string `json:"password_confirm"`
	InviteToken     string `json:"invite_token"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type Handler struct {
	service        Service
	loginProtector LoginProtector
}

type LoginProtector interface {
	AllowLogin(
		r *http.Request,
		username string,
	) (bool, time.Duration)
	RecordLoginFailure(r *http.Request, username string)
	RecordLoginSuccess(r *http.Request, username string)
}

func NewHandler(
	service Service,
	protectors ...LoginProtector,
) *Handler {
	handler := &Handler{service: service}
	if len(protectors) > 0 {
		handler.loginProtector = protectors[0]
	}

	return handler
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

	mux.HandleFunc(
		"POST /api/v1/auth/refresh",
		h.refresh,
	)
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var request registerRequest

	if !httpapi.DecodeJSON(w, r, &request) {
		return
	}

	result, err := h.service.Register(
		r.Context(),
		account.RegisterInput{
			Username:        request.Username,
			Password:        request.Password,
			PasswordConfirm: request.PasswordConfirm,
			InviteToken:     request.InviteToken,
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

		case errors.Is(err, account.ErrRegistrationInviteInvalid):
			httpapi.WriteError(
				w,
				http.StatusForbidden,
				"a valid registration invite is required",
			)

		case errors.Is(err, account.ErrUsernameTaken):
			httpapi.WriteError(
				w,
				http.StatusConflict,
				"username is already taken",
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

	if !httpapi.DecodeJSON(w, r, &request) {
		return
	}

	if h.loginProtector != nil {
		allowed, retryAfter := h.loginProtector.AllowLogin(
			r,
			request.Username,
		)
		if !allowed {
			seconds := max(
				1,
				int(retryAfter.Round(time.Second)/time.Second),
			)
			w.Header().Set(
				"Retry-After",
				strconv.Itoa(seconds),
			)
			httpapi.WriteError(
				w,
				http.StatusTooManyRequests,
				"too many failed login attempts; try again later",
			)
			return
		}
	}

	result, err := h.service.Login(
		r.Context(),
		account.LoginInput{
			Username: request.Username,
			Password: request.Password,
		},
	)
	if err != nil {
		if h.loginProtector != nil &&
			errors.Is(err, account.ErrInvalidCredentials) {

			h.loginProtector.RecordLoginFailure(
				r,
				request.Username,
			)
		}

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

	if h.loginProtector != nil {
		h.loginProtector.RecordLoginSuccess(
			r,
			request.Username,
		)
	}

	httpapi.WriteJSON(
		w,
		http.StatusOK,
		newAuthResponse(result),
	)
}

func (h *Handler) refresh(
	w http.ResponseWriter,
	r *http.Request,
) {
	currentToken, ok := bearerToken(r)
	if !ok {
		httpapi.WriteError(
			w,
			http.StatusUnauthorized,
			"authorization token is required",
		)
		return
	}

	refreshedSession, err := h.service.Refresh(
		r.Context(),
		currentToken,
	)
	if err != nil {
		if errors.Is(err, account.ErrUnauthorized) {
			httpapi.WriteError(
				w,
				http.StatusUnauthorized,
				"invalid or expired session",
			)
			return
		}

		log.Printf("refresh session: %v", err)

		httpapi.WriteError(
			w,
			http.StatusInternalServerError,
			"internal server error",
		)
		return
	}

	httpapi.WriteJSON(
		w,
		http.StatusOK,
		newSessionResponse(refreshedSession),
	)
}
