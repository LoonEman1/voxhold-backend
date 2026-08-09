package realtimehttp

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"voxhold-backend/internal/account"
	"voxhold-backend/internal/realtime"
)

const (
	authenticationTimeout = 5 * time.Second
	writeTimeout          = 3 * time.Second
	maxIncomingEventSize  = 16 * 1024
)

type Authenticator interface {
	Authenticate(
		ctx context.Context,
		token string,
	) (int64, error)
}

type Handler struct {
	authenticator Authenticator
}

func NewHandler(
	authenticator Authenticator,
) *Handler {
	return &Handler{
		authenticator: authenticator,
	}
}

func (h *Handler) RegisterRoutes(
	mux *http.ServeMux,
) {
	mux.HandleFunc(
		"GET /api/v1/ws",
		h.connect,
	)
}

func (h *Handler) connect(
	w http.ResponseWriter,
	r *http.Request,
) {
	connection, err := websocket.Accept(
		w,
		r,
		nil,
	)
	if err != nil {
		log.Printf(
			"accept websocket connection: %v",
			err,
		)
		return
	}
	defer connection.CloseNow()

	connection.SetReadLimit(maxIncomingEventSize)

	authContext, cancel := context.WithTimeout(
		context.Background(),
		authenticationTimeout,
	)
	defer cancel()

	var event realtime.IncomingEvent

	if err := wsjson.Read(
		authContext,
		connection,
		&event,
	); err != nil {
		_ = connection.Close(
			websocket.StatusPolicyViolation,
			"authentication required",
		)
		return
	}

	if event.Type != realtime.EventAuthenticate {
		h.writeError(
			connection,
			event.RequestID,
			realtime.ErrorUnauthorized,
			"authentication must be the first event",
		)

		_ = connection.Close(
			websocket.StatusPolicyViolation,
			"authentication required",
		)
		return
	}

	var authentication realtime.AuthenticateData

	if err := json.Unmarshal(
		event.Data,
		&authentication,
	); err != nil {
		h.writeError(
			connection,
			event.RequestID,
			realtime.ErrorInvalidPayload,
			"invalid authentication payload",
		)

		_ = connection.Close(
			websocket.StatusPolicyViolation,
			"invalid authentication payload",
		)
		return
	}

	authentication.Token = strings.TrimSpace(
		authentication.Token,
	)

	if authentication.Token == "" {
		h.writeError(
			connection,
			event.RequestID,
			realtime.ErrorUnauthorized,
			"session token is required",
		)

		_ = connection.Close(
			websocket.StatusPolicyViolation,
			"session token is required",
		)
		return
	}

	userID, err := h.authenticator.Authenticate(
		authContext,
		authentication.Token,
	)
	if err != nil {
		if errors.Is(err, account.ErrUnauthorized) {
			h.writeError(
				connection,
				event.RequestID,
				realtime.ErrorUnauthorized,
				"invalid or expired session",
			)

			_ = connection.Close(
				websocket.StatusPolicyViolation,
				"invalid or expired session",
			)
			return
		}

		log.Printf(
			"authenticate websocket connection: %v",
			err,
		)

		h.writeError(
			connection,
			event.RequestID,
			realtime.ErrorInternal,
			"internal server error",
		)

		_ = connection.Close(
			websocket.StatusInternalError,
			"internal server error",
		)
		return
	}

	if err := writeEvent(
		connection,
		realtime.OutgoingEvent{
			RequestID: event.RequestID,
			Type:      realtime.EventReady,
			Data: realtime.ReadyData{
				UserID:          userID,
				ProtocolVersion: realtime.ProtocolVersion,
			},
		},
	); err != nil {
		log.Printf(
			"write websocket ready event: %v",
			err,
		)
		return
	}

	_ = connection.Close(
		websocket.StatusNormalClosure,
		"authentication successful",
	)
}

func (h *Handler) writeError(
	connection *websocket.Conn,
	requestID string,
	code realtime.ErrorCode,
	message string,
) {
	if err := writeEvent(
		connection,
		realtime.OutgoingEvent{
			RequestID: requestID,
			Type:      realtime.EventError,
			Data: realtime.ErrorData{
				Code:    code,
				Message: message,
			},
		},
	); err != nil {
		log.Printf(
			"write websocket error event: %v",
			err,
		)
	}
}

func writeEvent(
	connection *websocket.Conn,
	event realtime.OutgoingEvent,
) error {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		writeTimeout,
	)
	defer cancel()

	return wsjson.Write(
		ctx,
		connection,
		event,
	)
}
