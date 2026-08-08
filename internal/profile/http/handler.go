package profilehttp

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"voxhold-backend/internal/httpapi"
	"voxhold-backend/internal/profile"
)

type Service interface {
	GetByUserID(
		ctx context.Context,
		userID int64,
	) (profile.Profile, error)

	Update(
		ctx context.Context,
		userID int64,
		input profile.UpdateInput,
	) (profile.Profile, error)

	GetVisibleByUserID(
		ctx context.Context,
		requesterUserID int64,
		targetUserID int64,
	) (profile.Profile, error)
}

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{
		service: service,
	}
}

func (h *Handler) RegisterRoutes(
	mux *http.ServeMux,
	requireAuth func(http.Handler) http.Handler,
) {
	mux.Handle(
		"GET /api/v1/me/profile",
		requireAuth(http.HandlerFunc(h.getCurrent)),
	)

	mux.Handle(
		"PATCH /api/v1/me/profile",
		requireAuth(http.HandlerFunc(h.updateCurrent)),
	)

	mux.Handle(
		"GET /api/v1/users/{userID}/profile",
		requireAuth(http.HandlerFunc(h.getByUserID)),
	)
}

type updateRequest struct {
	About       *string `json:"about"`
	CountryCode *string `json:"country_code"`
}

func (h *Handler) getCurrent(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	foundProfile, err := h.service.GetByUserID(
		r.Context(),
		userID,
	)
	if err != nil {
		log.Printf(
			"get profile for user %d: %v",
			userID,
			err,
		)

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
		newProfileResponse(foundProfile),
	)
}

func (h *Handler) updateCurrent(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	var request updateRequest

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

	updatedProfile, err := h.service.Update(
		r.Context(),
		userID,
		profile.UpdateInput{
			About:       request.About,
			CountryCode: request.CountryCode,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, profile.ErrNothingToUpdate),
			errors.Is(err, profile.ErrAboutTooLong),
			errors.Is(err, profile.ErrCountryCodeInvalid):

			httpapi.WriteError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)

		default:
			log.Printf(
				"update profile for user %d: %v",
				userID,
				err,
			)

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
		newProfileResponse(updatedProfile),
	)
}

func (h *Handler) getByUserID(
	w http.ResponseWriter,
	r *http.Request,
) {
	requesterUserID, ok := httpapi.AuthenticatedUserID(
		w,
		r,
	)
	if !ok {
		return
	}

	targetUserID, ok := httpapi.PositiveInt64PathValue(
		w,
		r,
		"userID",
		"user",
	)
	if !ok {
		return
	}

	foundProfile, err := h.service.GetVisibleByUserID(
		r.Context(),
		requesterUserID,
		targetUserID,
	)
	if err != nil {
		switch {
		case errors.Is(err, profile.ErrProfileNotFound):
			httpapi.WriteError(
				w,
				http.StatusNotFound,
				"profile not found",
			)

		default:
			log.Printf(
				"get profile %d for user %d: %v",
				targetUserID,
				requesterUserID,
				err,
			)

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
		newProfileResponse(foundProfile),
	)
}
