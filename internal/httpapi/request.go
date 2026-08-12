package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"

	"voxhold-backend/internal/account"
)

const MaxJSONBodyBytes int64 = 64 * 1024

func DecodeJSON(
	w http.ResponseWriter,
	r *http.Request,
	destination any,
) bool {
	r.Body = http.MaxBytesReader(
		w,
		r.Body,
		MaxJSONBodyBytes,
	)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(destination); err != nil {
		writeJSONDecodeError(w, err)
		return false
	}

	var trailingValue any
	if err := decoder.Decode(&trailingValue); !errors.Is(err, io.EOF) {
		writeJSONDecodeError(w, err)
		return false
	}

	return true
}

func writeJSONDecodeError(
	w http.ResponseWriter,
	err error,
) {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		WriteError(
			w,
			http.StatusRequestEntityTooLarge,
			"JSON body must not exceed 64 KiB",
		)
		return
	}

	WriteError(
		w,
		http.StatusBadRequest,
		"invalid JSON body",
	)
}

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
