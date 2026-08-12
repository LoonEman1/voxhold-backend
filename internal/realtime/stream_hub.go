package realtime

import "errors"

var (
	ErrStreamVoiceRequired = errors.New(
		"stream requires the same active voice channel",
	)
	ErrStreamAlreadyActive = errors.New(
		"stream is already active",
	)
	ErrStreamUnavailable = errors.New(
		"stream is not available",
	)
	ErrStreamViewerLimit = errors.New(
		"stream viewer limit reached",
	)
	ErrStreamP2PRelation = errors.New(
		"P2P stream peers are not related",
	)
)

type StreamSessionCloser interface {
	Leave(connectionID string)
}

func (h *Hub) SetStreamSessionCloser(
	closer StreamSessionCloser,
	maxServerViewers int,
	maxP2PViewers int,
) {
	h.streamSessions = closer
	if maxServerViewers > 0 {
		h.maxStreamViewers = maxServerViewers
	}
	if maxP2PViewers > 0 {
		h.maxP2PStreamViewers = maxP2PViewers
	}
}

func (h *Hub) StartStream(
	client *Client,
	serverID int64,
	channelID int64,
	mode StreamMode,
	codec StreamCodec,
	hasAudio bool,
) (StreamData, error) {
	if client == nil || (mode != StreamModeServer &&
		mode != StreamModeP2P) || !validStreamCodec(codec) {

		return StreamData{}, ErrStreamUnavailable
	}
	participant, exists := h.voice.current(client)
	if !exists || participant.ServerID != serverID ||
		participant.ChannelID != channelID {

		return StreamData{}, ErrStreamVoiceRequired
	}

	data, started := h.streams.start(
		client,
		participant,
		mode,
		codec,
		hasAudio,
	)
	if !started {
		return StreamData{}, ErrStreamAlreadyActive
	}

	current, stillInVoice := h.voice.current(client)
	if !stillInVoice || current.ServerID != serverID ||
		current.ChannelID != channelID {

		h.streams.leave(client)
		return StreamData{}, ErrStreamVoiceRequired
	}

	h.publishToServerExcept(
		serverID,
		client,
		OutgoingEvent{
			Type: EventStreamStarted,
			Data: data,
		},
	)
	return data, nil
}

func validStreamCodec(codec StreamCodec) bool {
	switch codec {
	case StreamCodecVP8, StreamCodecVP9,
		StreamCodecH264, StreamCodecAV1:
		return true
	default:
		return false
	}
}

func (h *Hub) WatchStream(
	client *Client,
	serverID int64,
	channelID int64,
) (StreamWatchingData, error) {
	if client == nil {
		return StreamWatchingData{}, ErrStreamUnavailable
	}
	participant, exists := h.voice.current(client)
	if !exists || participant.ServerID != serverID ||
		participant.ChannelID != channelID {

		return StreamWatchingData{}, ErrStreamVoiceRequired
	}

	data, publisher, err := h.streams.watch(
		client,
		participant,
		h.maxStreamViewers,
		h.maxP2PStreamViewers,
	)
	if err != nil {
		return StreamWatchingData{}, err
	}

	current, stillInVoice := h.voice.current(client)
	if !stillInVoice || current.ServerID != serverID ||
		current.ChannelID != channelID {

		h.streams.leave(client)
		return StreamWatchingData{}, ErrStreamVoiceRequired
	}

	if data.Mode == StreamModeP2P {
		if !publisher.enqueue(
			OutgoingEvent{
				Type: EventStreamViewerJoined,
				Data: StreamViewerData{
					ConnectionID: client.ConnectionID(),
					UserID:       client.UserID(),
				},
			},
		) {
			h.Unregister(publisher)
			return StreamWatchingData{}, ErrStreamUnavailable
		}
	}
	h.PublishToServer(
		serverID,
		OutgoingEvent{
			Type: EventStreamUpdated,
			Data: data,
		},
	)
	return StreamWatchingData{
		Stream:             data,
		ViewerConnectionID: client.ConnectionID(),
	}, nil
}

func (h *Hub) LeaveStream(
	client *Client,
	reason string,
) (StreamStoppedData, bool) {
	value, publisher, exists := h.leaveStream(client, reason)
	if !exists {
		return StreamStoppedData{}, false
	}
	return StreamStoppedData{
		ServerID:  value.serverID,
		ChannelID: value.channelID,
		Reason:    reason,
	}, publisher
}

func (h *Hub) leaveStream(
	client *Client,
	reason string,
) (*activeStream, bool, bool) {
	if client == nil {
		return nil, false, false
	}
	value, publisher, exists := h.streams.leave(client)
	if !exists {
		return nil, false, false
	}

	h.closeStreamSession(client.ConnectionID())
	if publisher {
		h.PublishToServer(
			value.serverID,
			OutgoingEvent{
				Type: EventStreamStopped,
				Data: StreamStoppedData{
					ServerID:  value.serverID,
					ChannelID: value.channelID,
					Reason:    reason,
				},
			},
		)
		return value, true, true
	}

	if value.mode == StreamModeP2P {
		if !value.publisher.enqueue(
			OutgoingEvent{
				Type: EventStreamViewerLeft,
				Data: StreamViewerData{
					ConnectionID: client.ConnectionID(),
					UserID:       client.UserID(),
				},
			},
		) {
			h.Unregister(value.publisher)
			return value, false, true
		}
	}
	h.PublishToServer(
		value.serverID,
		OutgoingEvent{
			Type: EventStreamUpdated,
			Data: h.streams.data(value),
		},
	)
	return value, false, true
}

func (h *Hub) stopStreamForChannel(
	channelID int64,
	reason string,
) {
	publisher := h.streams.publisherForChannel(channelID)
	if publisher != nil {
		h.leaveStream(publisher, reason)
	}
}

func (h *Hub) RelayStreamP2PSession(
	client *Client,
	targetConnectionID string,
	sdp string,
	typeOfEvent EventType,
) error {
	target, senderIsPublisher, ok := h.streams.relationship(
		client,
		targetConnectionID,
	)
	if !ok ||
		(typeOfEvent == EventStreamP2POffer && !senderIsPublisher) ||
		(typeOfEvent == EventStreamP2PAnswer && senderIsPublisher) {

		return ErrStreamP2PRelation
	}
	if !target.enqueue(
		OutgoingEvent{
			Type: typeOfEvent,
			Data: StreamP2PSessionData{
				FromConnectionID: client.ConnectionID(),
				SDP:              sdp,
			},
		},
	) {
		h.Unregister(target)
		return ErrStreamUnavailable
	}
	return nil
}

func (h *Hub) RelayStreamP2PICECandidate(
	client *Client,
	data StreamP2PICECandidateData,
) error {
	target, _, ok := h.streams.relationship(
		client,
		data.TargetConnectionID,
	)
	if !ok {
		return ErrStreamP2PRelation
	}
	data.FromConnectionID = client.ConnectionID()
	data.TargetConnectionID = ""
	if !target.enqueue(
		OutgoingEvent{
			Type: EventStreamP2PICECandidate,
			Data: data,
		},
	) {
		h.Unregister(target)
		return ErrStreamUnavailable
	}
	return nil
}

func (h *Hub) sendStreamSnapshot(client *Client) {
	if !client.enqueue(
		OutgoingEvent{
			Type: EventStreamSnapshot,
			Data: StreamSnapshotData{
				Streams: h.streams.snapshotForServers(
					client.serverIDs(),
				),
			},
		},
	) {
		h.Unregister(client)
	}
}

func (h *Hub) closeStreamSession(connectionID string) {
	if h.streamSessions != nil {
		h.streamSessions.Leave(connectionID)
	}
}
