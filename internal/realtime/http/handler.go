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
	"voxhold-backend/internal/channel"
	"voxhold-backend/internal/realtime"
)

const (
	authenticationTimeout = 5 * time.Second
	writeTimeout          = 3 * time.Second
	pingInterval          = 30 * time.Second
	maxIncomingEventSize  = 16 * 1024
)

type Authenticator interface {
	Authenticate(
		ctx context.Context,
		token string,
	) (int64, error)
}

type ChannelAccess interface {
	CheckAccess(
		ctx context.Context,
		serverID int64,
		channelID int64,
		userID int64,
	) error
}

type Handler struct {
	authenticator Authenticator
	channelAccess ChannelAccess
	hub           *realtime.Hub
}

func NewHandler(
	authenticator Authenticator,
	channelAccess ChannelAccess,
	hub *realtime.Hub,
) *Handler {
	return &Handler{
		authenticator: authenticator,
		channelAccess: channelAccess,
		hub:           hub,
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

	client := realtime.NewClient(userID)

	defer h.hub.Unregister(client)

	connectionContext, stopConnection :=
		context.WithCancel(context.Background())

	defer stopConnection()

	h.serveConnection(
		connectionContext,
		connection,
		client,
	)
}

func (h *Handler) serveConnection(
	parentContext context.Context,
	connection *websocket.Conn,
	client *realtime.Client,
) {
	ctx, cancel := context.WithCancel(parentContext)
	defer cancel()

	readErrors := make(chan error, 1)
	writeErrors := make(chan error, 1)

	go func() {
		readErrors <- h.readLoop(
			ctx,
			connection,
			client,
		)
	}()

	go func() {
		writeErrors <- h.writeLoop(
			ctx,
			connection,
			client,
		)
	}()

	var connectionError error

	select {
	case connectionError = <-readErrors:
	case connectionError = <-writeErrors:
	}

	cancel()
	client.Close()

	if connectionError == nil {
		return
	}

	closeStatus := websocket.CloseStatus(
		connectionError,
	)

	if errors.Is(connectionError, context.Canceled) ||
		closeStatus == websocket.StatusNormalClosure ||
		closeStatus == websocket.StatusGoingAway {

		return
	}

	log.Printf(
		"websocket connection for user %d: %v",
		client.UserID(),
		connectionError,
	)
}

func (h *Handler) readLoop(
	ctx context.Context,
	connection *websocket.Conn,
	client *realtime.Client,
) error {
	for {
		var event realtime.IncomingEvent

		if err := wsjson.Read(
			ctx,
			connection,
			&event,
		); err != nil {
			return err
		}

		if err := h.handleIncomingEvent(
			ctx,
			client,
			event,
		); err != nil {
			return err
		}
	}
}

func (h *Handler) writeLoop(
	ctx context.Context,
	connection *websocket.Conn,
	client *realtime.Client,
) error {
	pingTicker := time.NewTicker(pingInterval)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-client.Done():
			return nil

		case event := <-client.Outgoing():
			writeContext, cancel :=
				context.WithTimeout(
					ctx,
					writeTimeout,
				)

			err := wsjson.Write(
				writeContext,
				connection,
				event,
			)

			cancel()

			if err != nil {
				return err
			}

		case <-pingTicker.C:
			pingContext, cancel :=
				context.WithTimeout(
					ctx,
					writeTimeout,
				)

			err := connection.Ping(pingContext)

			cancel()

			if err != nil {
				return err
			}
		}
	}
}

func (h *Handler) handleIncomingEvent(
	ctx context.Context,
	client *realtime.Client,
	event realtime.IncomingEvent,
) error {
	switch event.Type {
	case realtime.EventChannelSubscribe:
		return h.subscribeToChannel(
			ctx,
			client,
			event,
		)

	case realtime.EventChannelUnsubscribe:
		return h.unsubscribeFromChannel(
			client,
			event,
		)

	default:
		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInvalidEvent,
			"unsupported event type",
		)
	}
}

func (h *Handler) subscribeToChannel(
	ctx context.Context,
	client *realtime.Client,
	event realtime.IncomingEvent,
) error {
	var data realtime.ChannelSubscriptionData

	if err := json.Unmarshal(
		event.Data,
		&data,
	); err != nil {
		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInvalidPayload,
			"invalid channel subscription payload",
		)
	}

	if data.ServerID <= 0 || data.ChannelID <= 0 {
		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInvalidPayload,
			"server_id and channel_id must be positive",
		)
	}

	if err := h.channelAccess.CheckAccess(
		ctx,
		data.ServerID,
		data.ChannelID,
		client.UserID(),
	); err != nil {
		switch {
		case errors.Is(err, channel.ErrNotFound),
			errors.Is(err, channel.ErrForbidden):

			return queueError(
				client,
				event.RequestID,
				realtime.ErrorForbidden,
				"not allowed to subscribe to channel",
			)

		default:
			log.Printf(
				"check channel subscription access: %v",
				err,
			)

			return queueError(
				client,
				event.RequestID,
				realtime.ErrorInternal,
				"internal server error",
			)
		}
	}

	h.hub.Subscribe(
		client,
		data.ChannelID,
	)

	return queueEvent(
		client,
		realtime.OutgoingEvent{
			RequestID: event.RequestID,
			Type:      realtime.EventChannelSubscribed,
			Data:      data,
		},
	)
}

func (h *Handler) unsubscribeFromChannel(
	client *realtime.Client,
	event realtime.IncomingEvent,
) error {
	var data realtime.ChannelSubscriptionData

	if err := json.Unmarshal(
		event.Data,
		&data,
	); err != nil {
		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInvalidPayload,
			"invalid channel unsubscription payload",
		)
	}

	if data.ServerID <= 0 || data.ChannelID <= 0 {
		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInvalidPayload,
			"server_id and channel_id must be positive",
		)
	}

	h.hub.Unsubscribe(
		client,
		data.ChannelID,
	)

	return queueEvent(
		client,
		realtime.OutgoingEvent{
			RequestID: event.RequestID,
			Type:      realtime.EventChannelUnsubscribed,
			Data:      data,
		},
	)
}

func queueError(
	client *realtime.Client,
	requestID string,
	code realtime.ErrorCode,
	message string,
) error {
	return queueEvent(
		client,
		realtime.OutgoingEvent{
			RequestID: requestID,
			Type:      realtime.EventError,
			Data: realtime.ErrorData{
				Code:    code,
				Message: message,
			},
		},
	)
}

func queueEvent(
	client *realtime.Client,
	event realtime.OutgoingEvent,
) error {
	if client.Send(event) {
		return nil
	}

	return errors.New(
		"websocket client outgoing queue is full",
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
