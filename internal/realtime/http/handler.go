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
	"voxhold-backend/internal/readstate"
	"voxhold-backend/internal/realtime"
	serverDomain "voxhold-backend/internal/server"
	"voxhold-backend/internal/stream"
	"voxhold-backend/internal/voice"
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

	CheckVoiceAccess(
		ctx context.Context,
		serverID int64,
		channelID int64,
		userID int64,
	) error
}

type MembershipLister interface {
	ListByUserID(
		ctx context.Context,
		userID int64,
	) ([]serverDomain.JoinedServer, error)
}

type ReadStateLister interface {
	ListByUserID(
		ctx context.Context,
		userID int64,
	) ([]readstate.ChannelRead, error)

	ListByChannelID(
		ctx context.Context,
		serverID int64,
		channelID int64,
		requesterUserID int64,
	) ([]readstate.ChannelRead, error)
}

type VoiceMedia interface {
	Join(
		connectionID string,
		userID int64,
		serverID int64,
		channelID int64,
		selfMute bool,
		selfDeaf bool,
	) error

	SetState(
		connectionID string,
		selfMute bool,
		selfDeaf bool,
	) error

	AcceptAnswer(connectionID string, sdp string) error

	AddICECandidate(
		connectionID string,
		candidate voice.ICECandidate,
	) error
}

type StreamMedia interface {
	Start(
		connectionID string,
		userID int64,
		serverID int64,
		channelID int64,
		codec stream.Codec,
		hasAudio bool,
	) error

	Watch(
		connectionID string,
		userID int64,
		serverID int64,
		channelID int64,
	) error

	AcceptAnswer(connectionID string, sdp string) error

	AddICECandidate(
		connectionID string,
		candidate stream.ICECandidate,
	) error
}

type Handler struct {
	authenticator Authenticator
	channelAccess ChannelAccess
	memberships   MembershipLister
	readStates    ReadStateLister
	voiceMedia    VoiceMedia
	streamMedia   StreamMedia
	hub           *realtime.Hub
	voiceJoins    userLockSet
}

func NewHandler(
	authenticator Authenticator,
	channelAccess ChannelAccess,
	memberships MembershipLister,
	readStates ReadStateLister,
	voiceMedia VoiceMedia,
	streamMedia StreamMedia,
	hub *realtime.Hub,
) *Handler {
	return &Handler{
		authenticator: authenticator,
		channelAccess: channelAccess,
		memberships:   memberships,
		readStates:    readStates,
		voiceMedia:    voiceMedia,
		streamMedia:   streamMedia,
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

	joinedServers, err := h.memberships.ListByUserID(
		authContext,
		userID,
	)
	if err != nil {
		log.Printf(
			"list websocket user memberships: %v",
			err,
		)
		_ = connection.Close(
			websocket.StatusInternalError,
			"internal server error",
		)
		return
	}

	serverIDs := make([]int64, 0, len(joinedServers))
	for _, joinedServer := range joinedServers {
		serverIDs = append(serverIDs, joinedServer.ID)
	}

	userReads, err := h.readStates.ListByUserID(
		authContext,
		userID,
	)
	if err != nil {
		log.Printf(
			"list websocket user read states: %v",
			err,
		)
		_ = connection.Close(
			websocket.StatusInternalError,
			"internal server error",
		)
		return
	}

	client := realtime.NewClient(
		userID,
		authentication.Token,
		serverIDs,
	)

	if !h.hub.Register(client) {
		_ = connection.Close(
			websocket.StatusInternalError,
			"failed to register connection",
		)
		return
	}

	defer h.hub.Unregister(client)

	confirmedUserID, err := h.authenticator.Authenticate(
		authContext,
		authentication.Token,
	)
	if err != nil || confirmedUserID != userID {
		status := websocket.StatusInternalError
		reason := "internal server error"

		if errors.Is(err, account.ErrUnauthorized) ||
			confirmedUserID != userID {

			status = websocket.StatusPolicyViolation
			reason = "invalid or expired session"
		} else {
			log.Printf(
				"confirm websocket session: %v",
				err,
			)
		}

		_ = connection.Close(status, reason)
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

	if err := queueEvent(
		client,
		realtime.OutgoingEvent{
			Type: realtime.EventReadSnapshot,
			Data: realtime.NewReadSnapshotData(
				userReads,
			),
		},
	); err != nil {
		log.Printf(
			"queue websocket read snapshot: %v",
			err,
		)
		return
	}

	connectionContext, stopConnection :=
		context.WithCancel(context.Background())

	defer stopConnection()

	h.serveConnection(
		connectionContext,
		connection,
		client,
		authentication.Token,
	)
}

func (h *Handler) serveConnection(
	parentContext context.Context,
	connection *websocket.Conn,
	client *realtime.Client,
	sessionToken string,
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
			sessionToken,
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
	sessionToken string,
) error {
	pingTicker := time.NewTicker(pingInterval)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-client.Done():
			if reason := client.CloseReason(); reason != "" {
				_ = connection.Close(
					websocket.StatusPolicyViolation,
					reason,
				)
			}

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

			authenticatedUserID, err :=
				h.authenticator.Authenticate(
					pingContext,
					sessionToken,
				)

			if err == nil &&
				authenticatedUserID != client.UserID() {

				err = account.ErrUnauthorized
			}

			if err == nil {
				err = connection.Ping(pingContext)
			}

			cancel()

			if errors.Is(err, account.ErrUnauthorized) {
				_ = connection.Close(
					websocket.StatusPolicyViolation,
					"session expired or revoked",
				)
				return nil
			}

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

	case realtime.EventVoiceJoin:
		return h.joinVoice(
			ctx,
			client,
			event,
		)

	case realtime.EventVoiceStateUpdate:
		return h.updateVoiceState(
			client,
			event,
		)

	case realtime.EventVoiceLeave:
		return h.leaveVoice(client, event)

	case realtime.EventVoiceWebRTCAnswer:
		return h.acceptVoiceWebRTCAnswer(
			client,
			event,
		)

	case realtime.EventVoiceICECandidate:
		return h.addVoiceICECandidate(
			client,
			event,
		)

	case realtime.EventStreamStart:
		return h.startStream(client, event)

	case realtime.EventStreamWatch:
		return h.watchStream(client, event)

	case realtime.EventStreamStop,
		realtime.EventStreamLeave:

		return h.leaveStream(client, event)

	case realtime.EventStreamWebRTCAnswer:
		return h.acceptStreamWebRTCAnswer(client, event)

	case realtime.EventStreamICECandidate:
		return h.addStreamICECandidate(client, event)

	case realtime.EventStreamP2POffer,
		realtime.EventStreamP2PAnswer:

		return h.relayStreamP2PSession(client, event)

	case realtime.EventStreamP2PICECandidate:
		return h.relayStreamP2PICECandidate(client, event)

	default:
		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInvalidEvent,
			"unsupported event type",
		)
	}
}

func (h *Handler) joinVoice(
	ctx context.Context,
	client *realtime.Client,
	event realtime.IncomingEvent,
) error {
	var data realtime.VoiceJoinData

	if err := json.Unmarshal(event.Data, &data); err != nil {
		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInvalidPayload,
			"invalid voice join payload",
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

	allowed, err := h.checkVoiceChannelAccess(
		ctx,
		client,
		event.RequestID,
		data.ServerID,
		data.ChannelID,
	)
	if err != nil || !allowed {
		return err
	}

	unlockVoiceJoin := h.voiceJoins.lock(client.UserID())
	defer unlockVoiceJoin()

	allowed, err = h.checkVoiceChannelAccess(
		ctx,
		client,
		event.RequestID,
		data.ServerID,
		data.ChannelID,
	)
	if err != nil || !allowed {
		return err
	}

	joined, ok := h.hub.JoinVoice(
		client,
		data.ServerID,
		data.ChannelID,
		data.SelfMute,
		data.SelfDeaf,
	)
	if !ok {
		return queueError(
			client,
			event.RequestID,
			realtime.ErrorForbidden,
			"not allowed to join voice channel",
		)
	}

	if err := queueEvent(
		client,
		realtime.OutgoingEvent{
			RequestID: event.RequestID,
			Type:      realtime.EventVoiceJoined,
			Data:      joined,
		},
	); err != nil {
		return err
	}

	if err := h.voiceMedia.Join(
		client.ConnectionID(),
		client.UserID(),
		data.ServerID,
		data.ChannelID,
		data.SelfMute,
		data.SelfDeaf,
	); err != nil {
		h.hub.LeaveVoice(client)

		if errors.Is(err, voice.ErrRoomFull) {
			return queueError(
				client,
				event.RequestID,
				realtime.ErrorInvalidState,
				"voice channel is full",
			)
		}

		log.Printf("start WebRTC voice session: %v", err)

		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInternal,
			"failed to start voice media session",
		)
	}

	return nil
}

func (h *Handler) updateVoiceState(
	client *realtime.Client,
	event realtime.IncomingEvent,
) error {
	var data realtime.VoiceStateUpdateData

	if err := json.Unmarshal(event.Data, &data); err != nil {
		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInvalidPayload,
			"invalid voice state payload",
		)
	}

	if err := h.voiceMedia.SetState(
		client.ConnectionID(),
		data.SelfMute,
		data.SelfDeaf,
	); err != nil {
		if errors.Is(err, voice.ErrSessionNotFound) {
			h.hub.LeaveVoice(client)
			return queueError(
				client,
				event.RequestID,
				realtime.ErrorInvalidState,
				"WebRTC voice session is not active",
			)
		}

		log.Printf("update WebRTC voice state: %v", err)
		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInternal,
			"failed to update voice media state",
		)
	}

	participant, ok := h.hub.UpdateVoiceState(
		client,
		data.SelfMute,
		data.SelfDeaf,
	)
	if !ok {
		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInvalidState,
			"voice channel is not joined",
		)
	}

	return queueEvent(
		client,
		realtime.OutgoingEvent{
			RequestID: event.RequestID,
			Type:      realtime.EventVoiceStateUpdated,
			Data:      participant,
		},
	)
}

func (h *Handler) acceptVoiceWebRTCAnswer(
	client *realtime.Client,
	event realtime.IncomingEvent,
) error {
	var data realtime.VoiceWebRTCAnswerData

	if err := json.Unmarshal(event.Data, &data); err != nil ||
		strings.TrimSpace(data.SDP) == "" {

		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInvalidPayload,
			"invalid WebRTC answer payload",
		)
	}

	if err := h.voiceMedia.AcceptAnswer(
		client.ConnectionID(),
		data.SDP,
	); err != nil {
		return h.handleVoiceMediaInputError(
			client,
			event.RequestID,
			"accept WebRTC answer",
			err,
		)
	}

	return queueEvent(
		client,
		realtime.OutgoingEvent{
			RequestID: event.RequestID,
			Type:      realtime.EventVoiceWebRTCAnswered,
			Data: realtime.VoiceWebRTCAnsweredData{
				Accepted: true,
			},
		},
	)
}

func (h *Handler) addVoiceICECandidate(
	client *realtime.Client,
	event realtime.IncomingEvent,
) error {
	var data realtime.VoiceICECandidateData

	if err := json.Unmarshal(event.Data, &data); err != nil ||
		strings.TrimSpace(data.Candidate) == "" {

		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInvalidPayload,
			"invalid ICE candidate payload",
		)
	}

	err := h.voiceMedia.AddICECandidate(
		client.ConnectionID(),
		voice.ICECandidate{
			Candidate:        data.Candidate,
			SDPMid:           data.SDPMid,
			SDPMLineIndex:    data.SDPMLineIndex,
			UsernameFragment: data.UsernameFragment,
		},
	)
	if err != nil {
		return h.handleVoiceMediaInputError(
			client,
			event.RequestID,
			"add WebRTC ICE candidate",
			err,
		)
	}

	return nil
}

func (h *Handler) handleVoiceMediaInputError(
	client *realtime.Client,
	requestID string,
	operation string,
	err error,
) error {
	if errors.Is(err, voice.ErrSessionNotFound) {
		return queueError(
			client,
			requestID,
			realtime.ErrorInvalidState,
			"WebRTC voice session is not active",
		)
	}
	if errors.Is(err, voice.ErrTooManyICECandidates) {
		h.hub.LeaveVoice(client)
		return queueError(
			client,
			requestID,
			realtime.ErrorInvalidState,
			"too many pending ICE candidates",
		)
	}

	log.Printf("%s: %v", operation, err)
	return queueError(
		client,
		requestID,
		realtime.ErrorInvalidPayload,
		"invalid WebRTC signaling payload",
	)
}

func (h *Handler) leaveVoice(
	client *realtime.Client,
	event realtime.IncomingEvent,
) error {
	left, ok := h.hub.LeaveVoice(client)
	if !ok {
		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInvalidState,
			"voice channel is not joined",
		)
	}

	return queueEvent(
		client,
		realtime.OutgoingEvent{
			RequestID: event.RequestID,
			Type:      realtime.EventVoiceLeft,
			Data:      left,
		},
	)
}

func (h *Handler) checkVoiceChannelAccess(
	ctx context.Context,
	client *realtime.Client,
	requestID string,
	serverID int64,
	channelID int64,
) (bool, error) {
	err := h.channelAccess.CheckVoiceAccess(
		ctx,
		serverID,
		channelID,
		client.UserID(),
	)
	if err == nil {
		return true, nil
	}

	switch {
	case errors.Is(err, channel.ErrVoiceRequired):
		return false, queueError(
			client,
			requestID,
			realtime.ErrorInvalidState,
			"channel is not a voice channel",
		)

	case errors.Is(err, channel.ErrNotFound),
		errors.Is(err, channel.ErrForbidden):

		return false, queueError(
			client,
			requestID,
			realtime.ErrorForbidden,
			"not allowed to join voice channel",
		)

	default:
		log.Printf("check voice channel access: %v", err)

		return false, queueError(
			client,
			requestID,
			realtime.ErrorInternal,
			"internal server error",
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

	allowed, err := h.checkChannelAccess(
		ctx,
		client,
		event.RequestID,
		data,
	)
	if err != nil || !allowed {
		return err
	}

	h.hub.Subscribe(
		client,
		data.ServerID,
		data.ChannelID,
	)

	allowed, err = h.checkChannelAccess(
		ctx,
		client,
		event.RequestID,
		data,
	)
	if err != nil || !allowed {
		h.hub.Unsubscribe(
			client,
			data.ChannelID,
		)
		return err
	}

	channelReads, err := h.readStates.ListByChannelID(
		ctx,
		data.ServerID,
		data.ChannelID,
		client.UserID(),
	)
	sendReadSnapshot := true
	if err != nil {
		if errors.Is(err, readstate.ErrTextChannelRequired) {
			sendReadSnapshot = false
		} else {
			h.hub.Unsubscribe(client, data.ChannelID)

			if errors.Is(err, readstate.ErrForbidden) ||
				errors.Is(err, readstate.ErrChannelNotFound) {

				return queueError(
					client,
					event.RequestID,
					realtime.ErrorForbidden,
					"not allowed to subscribe to channel",
				)
			}

			log.Printf(
				"list channel read snapshot: %v",
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

	if err := queueEvent(
		client,
		realtime.OutgoingEvent{
			RequestID: event.RequestID,
			Type:      realtime.EventChannelSubscribed,
			Data:      data,
		},
	); err != nil {
		return err
	}

	if !sendReadSnapshot {
		return nil
	}

	return queueEvent(
		client,
		realtime.OutgoingEvent{
			Type: realtime.EventChannelReadSnapshot,
			Data: realtime.NewChannelReadSnapshotData(
				data.ServerID,
				data.ChannelID,
				channelReads,
			),
		},
	)
}

func (h *Handler) checkChannelAccess(
	ctx context.Context,
	client *realtime.Client,
	requestID string,
	data realtime.ChannelSubscriptionData,
) (bool, error) {
	err := h.channelAccess.CheckAccess(
		ctx,
		data.ServerID,
		data.ChannelID,
		client.UserID(),
	)
	if err == nil {
		return true, nil
	}

	if errors.Is(err, channel.ErrNotFound) ||
		errors.Is(err, channel.ErrForbidden) {

		return false, queueError(
			client,
			requestID,
			realtime.ErrorForbidden,
			"not allowed to subscribe to channel",
		)
	}

	log.Printf(
		"check channel subscription access: %v",
		err,
	)

	return false, queueError(
		client,
		requestID,
		realtime.ErrorInternal,
		"internal server error",
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
