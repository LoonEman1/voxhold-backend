package httpapi

import (
	"encoding/json"
	"net/http"
)

func WriteJSON(
	w http.ResponseWriter,
	status int,
	value any,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(value)
}

func WriteError(
	w http.ResponseWriter,
	status int,
	message string,
) {
	WriteJSON(w, status, map[string]string{
		"error": message,
	})
}
