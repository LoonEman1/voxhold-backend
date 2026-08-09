package messagehttp

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"voxhold-backend/internal/httpapi"
	"voxhold-backend/internal/message"
)

func (h *Handler) search(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	serverID, ok := httpapi.PositiveInt64PathValue(
		w, r, "serverID", "server",
	)
	if !ok {
		return
	}

	limit, ok := optionalPositiveIntQuery(
		w, r, "limit",
	)
	if !ok {
		return
	}

	beforeID, ok := optionalPositiveInt64Query(
		w, r, "before_id",
	)
	if !ok {
		return
	}

	page, err := h.service.Search(
		r.Context(),
		serverID,
		userID,
		message.SearchInput{
			Query:    r.URL.Query().Get("q"),
			BeforeID: beforeID,
			Limit:    limit,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, message.ErrSearchQueryRequired),
			errors.Is(err, message.ErrSearchQueryTooLong),
			errors.Is(err, message.ErrSearchQueryInvalid),
			errors.Is(err, message.ErrSearchLimitInvalid),
			errors.Is(err, message.ErrBeforeIDInvalid):

			httpapi.WriteError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)

		case errors.Is(err, message.ErrForbidden):
			httpapi.WriteError(
				w,
				http.StatusForbidden,
				"not allowed to search messages",
			)

		default:
			log.Printf(
				"search messages in server %d: %v",
				serverID,
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
		newSearchPageResponse(page),
	)
}

func (h *Handler) getContext(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	serverID, channelID, messageID, ok :=
		messagePathValues(w, r)
	if !ok {
		return
	}

	before, ok := optionalPositiveIntQuery(
		w, r, "before",
	)
	if !ok {
		return
	}

	after, ok := optionalPositiveIntQuery(
		w, r, "after",
	)
	if !ok {
		return
	}

	contextMessages, err := h.service.GetContext(
		r.Context(),
		serverID,
		channelID,
		messageID,
		userID,
		message.ContextInput{
			Before: before,
			After:  after,
		},
	)
	if err != nil {
		if errors.Is(err, message.ErrContextLimitInvalid) {
			httpapi.WriteError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)
			return
		}

		writeMessageOperationError(
			w,
			"get message context",
			channelID,
			err,
		)
		return
	}

	httpapi.WriteJSON(
		w,
		http.StatusOK,
		newMessageContextResponse(contextMessages),
	)
}

func optionalPositiveIntQuery(
	w http.ResponseWriter,
	r *http.Request,
	name string,
) (int, bool) {
	rawValue := r.URL.Query().Get(name)
	if rawValue == "" {
		return 0, true
	}

	value, err := strconv.ParseInt(rawValue, 10, 32)
	if err != nil || value <= 0 {
		httpapi.WriteError(
			w,
			http.StatusBadRequest,
			name+" must be a positive integer",
		)
		return 0, false
	}

	return int(value), true
}

func optionalPositiveInt64Query(
	w http.ResponseWriter,
	r *http.Request,
	name string,
) (*int64, bool) {
	rawValue := r.URL.Query().Get(name)
	if rawValue == "" {
		return nil, true
	}

	value, err := strconv.ParseInt(rawValue, 10, 64)
	if err != nil || value <= 0 {
		httpapi.WriteError(
			w,
			http.StatusBadRequest,
			name+" must be a positive integer",
		)
		return nil, false
	}

	return &value, true
}
