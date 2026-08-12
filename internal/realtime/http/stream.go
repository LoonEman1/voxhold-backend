package realtimehttp

import (
	"encoding/json"
	"errors"
	"log"
	"strings"

	"voxhold-backend/internal/realtime"
	"voxhold-backend/internal/stream"
)

func (h *Handler) startStream(
	client *realtime.Client,
	event realtime.IncomingEvent,
) error {
	var data realtime.StreamStartData
	if err := json.Unmarshal(event.Data, &data); err != nil ||
		data.ServerID <= 0 || data.ChannelID <= 0 ||
		(data.Mode != realtime.StreamModeServer &&
			data.Mode != realtime.StreamModeP2P) ||
		!validStreamCodec(data.Codec) {

		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInvalidPayload,
			"invalid stream start payload",
		)
	}

	streamData, err := h.hub.StartStream(
		client,
		data.ServerID,
		data.ChannelID,
		data.Mode,
		data.Codec,
		data.HasAudio,
	)
	if err != nil {
		return queueStreamStateError(client, event.RequestID, err)
	}

	if data.Mode == realtime.StreamModeServer {
		err = h.streamMedia.Start(
			client.ConnectionID(),
			client.UserID(),
			data.ServerID,
			data.ChannelID,
			stream.Codec(data.Codec),
			data.HasAudio,
		)
		if err != nil {
			h.hub.LeaveStream(
				client,
				"failed to start stream media session",
			)
			log.Printf("start WebRTC stream publisher: %v", err)
			return queueError(
				client,
				event.RequestID,
				realtime.ErrorInternal,
				"failed to start stream media session",
			)
		}
	}

	return queueEvent(
		client,
		realtime.OutgoingEvent{
			RequestID: event.RequestID,
			Type:      realtime.EventStreamStarted,
			Data:      streamData,
		},
	)
}

func validStreamCodec(codec realtime.StreamCodec) bool {
	switch codec {
	case realtime.StreamCodecVP8, realtime.StreamCodecVP9,
		realtime.StreamCodecH264, realtime.StreamCodecAV1:
		return true
	default:
		return false
	}
}

func (h *Handler) watchStream(
	client *realtime.Client,
	event realtime.IncomingEvent,
) error {
	var data realtime.StreamWatchData
	if err := json.Unmarshal(event.Data, &data); err != nil ||
		data.ServerID <= 0 || data.ChannelID <= 0 {

		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInvalidPayload,
			"invalid stream watch payload",
		)
	}

	watching, err := h.hub.WatchStream(
		client,
		data.ServerID,
		data.ChannelID,
	)
	if err != nil {
		return queueStreamStateError(client, event.RequestID, err)
	}

	if watching.Stream.Mode == realtime.StreamModeServer {
		err = h.streamMedia.Watch(
			client.ConnectionID(),
			client.UserID(),
			data.ServerID,
			data.ChannelID,
		)
		if err != nil {
			h.hub.LeaveStream(
				client,
				"failed to start stream viewer session",
			)
			if errors.Is(err, stream.ErrViewerLimit) {
				return queueError(
					client,
					event.RequestID,
					realtime.ErrorInvalidState,
					"stream viewer limit reached",
				)
			}
			log.Printf("start WebRTC stream viewer: %v", err)
			return queueError(
				client,
				event.RequestID,
				realtime.ErrorInternal,
				"failed to start stream viewer session",
			)
		}
	}

	return queueEvent(
		client,
		realtime.OutgoingEvent{
			RequestID: event.RequestID,
			Type:      realtime.EventStreamWatching,
			Data:      watching,
		},
	)
}

func (h *Handler) leaveStream(
	client *realtime.Client,
	event realtime.IncomingEvent,
) error {
	data, publisher := h.hub.LeaveStream(
		client,
		"stream stopped by user",
	)
	if data.ChannelID == 0 {
		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInvalidState,
			"stream is not active on this connection",
		)
	}

	eventType := realtime.EventStreamLeft
	if publisher {
		eventType = realtime.EventStreamStopped
	}
	return queueEvent(
		client,
		realtime.OutgoingEvent{
			RequestID: event.RequestID,
			Type:      eventType,
			Data:      data,
		},
	)
}

func (h *Handler) acceptStreamWebRTCAnswer(
	client *realtime.Client,
	event realtime.IncomingEvent,
) error {
	var data realtime.StreamWebRTCAnswerData
	if err := json.Unmarshal(event.Data, &data); err != nil ||
		strings.TrimSpace(data.SDP) == "" {

		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInvalidPayload,
			"invalid stream WebRTC answer payload",
		)
	}
	if err := h.streamMedia.AcceptAnswer(
		client.ConnectionID(),
		data.SDP,
	); err != nil {
		return h.handleStreamMediaInputError(
			client,
			event.RequestID,
			"accept stream WebRTC answer",
			err,
		)
	}
	return queueEvent(
		client,
		realtime.OutgoingEvent{
			RequestID: event.RequestID,
			Type:      realtime.EventStreamWebRTCAnswered,
			Data: realtime.StreamWebRTCAnsweredData{
				Accepted: true,
			},
		},
	)
}

func (h *Handler) addStreamICECandidate(
	client *realtime.Client,
	event realtime.IncomingEvent,
) error {
	var data realtime.StreamICECandidateData
	if err := json.Unmarshal(event.Data, &data); err != nil ||
		strings.TrimSpace(data.Candidate) == "" {

		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInvalidPayload,
			"invalid stream ICE candidate payload",
		)
	}
	err := h.streamMedia.AddICECandidate(
		client.ConnectionID(),
		stream.ICECandidate{
			Candidate:        data.Candidate,
			SDPMid:           data.SDPMid,
			SDPMLineIndex:    data.SDPMLineIndex,
			UsernameFragment: data.UsernameFragment,
		},
	)
	if err != nil {
		return h.handleStreamMediaInputError(
			client,
			event.RequestID,
			"add stream WebRTC ICE candidate",
			err,
		)
	}
	return nil
}

func (h *Handler) handleStreamMediaInputError(
	client *realtime.Client,
	requestID string,
	operation string,
	err error,
) error {
	if errors.Is(err, stream.ErrSessionNotFound) {
		return queueError(
			client,
			requestID,
			realtime.ErrorInvalidState,
			"stream WebRTC session is not active",
		)
	}
	if errors.Is(err, stream.ErrTooManyICECandidates) {
		h.hub.LeaveStream(client, "too many pending ICE candidates")
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
		"invalid stream WebRTC signaling payload",
	)
}

func (h *Handler) relayStreamP2PSession(
	client *realtime.Client,
	event realtime.IncomingEvent,
) error {
	var data realtime.StreamP2PSessionData
	if err := json.Unmarshal(event.Data, &data); err != nil ||
		strings.TrimSpace(data.TargetConnectionID) == "" ||
		strings.TrimSpace(data.SDP) == "" {

		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInvalidPayload,
			"invalid P2P stream session payload",
		)
	}
	if err := h.hub.RelayStreamP2PSession(
		client,
		data.TargetConnectionID,
		data.SDP,
		event.Type,
	); err != nil {
		return queueStreamStateError(client, event.RequestID, err)
	}
	return nil
}

func (h *Handler) relayStreamP2PICECandidate(
	client *realtime.Client,
	event realtime.IncomingEvent,
) error {
	var data realtime.StreamP2PICECandidateData
	if err := json.Unmarshal(event.Data, &data); err != nil ||
		strings.TrimSpace(data.TargetConnectionID) == "" ||
		strings.TrimSpace(data.Candidate) == "" {

		return queueError(
			client,
			event.RequestID,
			realtime.ErrorInvalidPayload,
			"invalid P2P stream ICE candidate payload",
		)
	}
	if err := h.hub.RelayStreamP2PICECandidate(
		client,
		data,
	); err != nil {
		return queueStreamStateError(client, event.RequestID, err)
	}
	return nil
}

func queueStreamStateError(
	client *realtime.Client,
	requestID string,
	err error,
) error {
	message := "stream is not available"
	code := realtime.ErrorInvalidState
	switch {
	case errors.Is(err, realtime.ErrStreamVoiceRequired):
		message = "join the same voice channel first"
	case errors.Is(err, realtime.ErrStreamAlreadyActive):
		message = "stream is already active"
	case errors.Is(err, realtime.ErrStreamViewerLimit):
		message = "stream viewer limit reached"
	case errors.Is(err, realtime.ErrStreamP2PRelation):
		message = "P2P stream peer is not allowed"
		code = realtime.ErrorForbidden
	}
	return queueError(client, requestID, code, message)
}
