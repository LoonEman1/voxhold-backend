package diagnosticshttp

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"voxhold-backend/internal/diagnostics"
	"voxhold-backend/internal/httpapi"
)

type Service interface {
	Ingest(
		ctx context.Context,
		userID int64,
		input diagnostics.BatchInput,
	) (int, error)
	List(
		ctx context.Context,
		userID int64,
		filter diagnostics.ListFilter,
	) ([]diagnostics.Event, error)
}

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(
	mux *http.ServeMux,
	requireAuth func(http.Handler) http.Handler,
) {
	mux.Handle(
		"POST /api/v1/diagnostics/client-events",
		requireAuth(http.HandlerFunc(h.ingest)),
	)
	mux.Handle(
		"GET /api/v1/diagnostics/client-events",
		requireAuth(http.HandlerFunc(h.list)),
	)
}

type eventRequest struct {
	TimestampMS int64           `json:"timestamp_ms"`
	Category    string          `json:"category"`
	Name        string          `json:"name"`
	Level       string          `json:"level"`
	Details     json.RawMessage `json:"details"`
}

type batchRequest struct {
	SessionID     string         `json:"session_id"`
	ClientVersion string         `json:"client_version"`
	Platform      string         `json:"platform"`
	Events        []eventRequest `json:"events"`
}

func (h *Handler) ingest(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	var request batchRequest
	if !httpapi.DecodeJSON(w, r, &request) {
		return
	}

	input := diagnostics.BatchInput{
		SessionID:     request.SessionID,
		ClientVersion: request.ClientVersion,
		Platform:      request.Platform,
		Events:        make([]diagnostics.EventInput, 0, len(request.Events)),
	}
	for _, event := range request.Events {
		input.Events = append(input.Events, diagnostics.EventInput{
			ClientTimestampMS: event.TimestampMS,
			Category:          event.Category,
			Name:              event.Name,
			Level:             event.Level,
			DetailsJSON:       string(event.Details),
		})
	}

	accepted, err := h.service.Ingest(r.Context(), userID, input)
	if err != nil {
		if errors.Is(err, diagnostics.ErrInvalidBatch) ||
			errors.Is(err, diagnostics.ErrInvalidEvent) {
			httpapi.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		log.Printf("ingest client diagnostics for user %d: %v", userID, err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	httpapi.WriteJSON(w, http.StatusAccepted, map[string]int{"accepted": accepted})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}

	filter := diagnostics.ListFilter{
		SessionID: r.URL.Query().Get("session_id"),
		Category:  r.URL.Query().Get("category"),
	}
	var err error
	if value := r.URL.Query().Get("since"); value != "" {
		filter.Since, err = strconv.ParseInt(value, 10, 64)
		if err != nil {
			httpapi.WriteError(w, http.StatusBadRequest, "invalid diagnostics since value")
			return
		}
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		filter.Limit, err = strconv.Atoi(value)
		if err != nil {
			httpapi.WriteError(w, http.StatusBadRequest, "invalid diagnostics limit")
			return
		}
	}

	events, err := h.service.List(r.Context(), userID, filter)
	if err != nil {
		switch {
		case errors.Is(err, diagnostics.ErrForbidden):
			httpapi.WriteError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, diagnostics.ErrInvalidBatch):
			httpapi.WriteError(w, http.StatusBadRequest, err.Error())
		default:
			log.Printf("list client diagnostics for user %d: %v", userID, err)
			httpapi.WriteError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	response := make([]eventResponse, 0, len(events))
	for _, event := range events {
		response = append(response, newEventResponse(event))
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"retention_seconds": int64(diagnostics.Retention.Seconds()),
		"events":            response,
	})
}

type eventResponse struct {
	ID                int64           `json:"id"`
	CreatedAt         int64           `json:"created_at"`
	ClientTimestampMS int64           `json:"client_timestamp_ms"`
	UserID            int64           `json:"user_id"`
	Username          string          `json:"username"`
	SessionID         string          `json:"session_id"`
	ClientVersion     string          `json:"client_version"`
	Platform          string          `json:"platform"`
	Category          string          `json:"category"`
	Name              string          `json:"name"`
	Level             string          `json:"level"`
	Details           json.RawMessage `json:"details"`
}

func newEventResponse(event diagnostics.Event) eventResponse {
	return eventResponse{
		ID:                event.ID,
		CreatedAt:         event.CreatedAt,
		ClientTimestampMS: event.ClientTimestampMS,
		UserID:            event.UserID,
		Username:          event.Username,
		SessionID:         event.SessionID,
		ClientVersion:     event.ClientVersion,
		Platform:          event.Platform,
		Category:          event.Category,
		Name:              event.Name,
		Level:             event.Level,
		Details:           event.Details,
	}
}
