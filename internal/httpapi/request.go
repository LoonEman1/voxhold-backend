package httpapi

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	"voxhold-backend/internal/account"
)

func AuthenticatedUserID(
	w http.ResponseWriter,
	r *http.Request,
) (int64, bool) {
	userID, ok := account.UserIDFromContext(r.Context())
	if !ok || userID <= 0 {
		log.Printf(
			"%s %s: authenticated user ID is missing from context",
			r.Method,
			r.URL.Path,
		)

		WriteError(
			w,
			http.StatusInternalServerError,
			"internal server error",
		)
		return 0, false
	}

	return userID, true
}

func PositiveInt64PathValue(
	w http.ResponseWriter,
	r *http.Request,
	pathName string,
	entityName string,
) (int64, bool) {
	value, err := strconv.ParseInt(
		r.PathValue(pathName),
		10,
		64,
	)
	if err != nil || value <= 0 {
		WriteError(
			w,
			http.StatusBadRequest,
			fmt.Sprintf("invalid %s ID", entityName),
		)
		return 0, false
	}

	return value, true
}
