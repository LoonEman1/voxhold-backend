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
	ErrStreamRenditionsInvalid = errors.New(
		"stream renditions are invalid",
	)
	ErrStreamRenditionUnavailable = errors.New(
		"compatible stream rendition is not available",
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
	return h.StartStreamWithRenditions(
		client,
		serverID,
		channelID,
		mode,
		codec,
		hasAudio,
		nil,
	)
}

func (h *Hub) StartStreamWithRenditions(
	client *Client,
	serverID int64,
	channelID int64,
	mode StreamMode,
	codec StreamCodec,
	hasAudio bool,
	renditions []StreamRenditionData,
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
	normalizedRenditions, ok := normalizeStreamRenditions(codec, renditions)
	if !ok || (mode == StreamModeP2P && !onlySDRStreamRenditions(normalizedRenditions)) {
		return StreamData{}, ErrStreamRenditionsInvalid
	}

	data, started := h.streams.start(
		client,
		participant,
		mode,
		codec,
		hasAudio,
		normalizedRenditions,
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
	return h.WatchStreamWithCapabilities(client, serverID, channelID, nil, nil)
}

func (h *Hub) WatchStreamWithCapabilities(
	client *Client,
	serverID int64,
	channelID int64,
	supportedDynamicRanges []StreamDynamicRange,
	codecProfiles []StreamCodecProfileData,
) (StreamWatchingData, error) {
	if client == nil {
		return StreamWatchingData{}, ErrStreamUnavailable
	}
	participant, exists := h.voice.current(client)
	if !exists || participant.ServerID != serverID ||
		participant.ChannelID != channelID {

		return StreamWatchingData{}, ErrStreamVoiceRequired
	}

	data, publisher, selectedRenditionID, err := h.streams.watch(
		client,
		participant,
		h.maxStreamViewers,
		h.maxP2PStreamViewers,
		supportedDynamicRanges,
		codecProfiles,
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
		Stream:              data,
		ViewerConnectionID:  client.ConnectionID(),
		SelectedRenditionID: selectedRenditionID,
	}, nil
}

func normalizeStreamRenditions(
	legacyCodec StreamCodec,
	values []StreamRenditionData,
) ([]StreamRenditionData, bool) {
	if len(values) == 0 {
		return []StreamRenditionData{legacySDRStreamRendition(legacyCodec)}, true
	}
	if len(values) > MaxStreamRenditions {
		return nil, false
	}
	result := cloneStreamRenditions(values)
	seenIDs := make(map[string]struct{}, len(result))
	seenRanges := make(map[StreamDynamicRange]struct{}, len(result))
	for _, value := range result {
		if !validStreamRenditionID(value.ID) || !validStreamRendition(value) {
			return nil, false
		}
		if _, exists := seenIDs[value.ID]; exists {
			return nil, false
		}
		if _, exists := seenRanges[value.DynamicRange]; exists {
			return nil, false
		}
		seenIDs[value.ID] = struct{}{}
		seenRanges[value.DynamicRange] = struct{}{}
	}
	return result, true
}

func legacySDRStreamRendition(codec StreamCodec) StreamRenditionData {
	profile := ""
	switch codec {
	case StreamCodecVP9:
		profile = "0"
	case StreamCodecH264:
		profile = "baseline"
	case StreamCodecAV1:
		profile = "main"
	}
	return StreamRenditionData{
		ID:             "sdr",
		Codec:          codec,
		Profile:        profile,
		DynamicRange:   StreamDynamicRangeSDR,
		BitDepth:       8,
		ColorPrimaries: StreamColorPrimariesBT709,
		Transfer:       StreamTransferBT709,
		Matrix:         StreamMatrixBT709,
	}
}

func validStreamRenditionID(value string) bool {
	if len(value) == 0 || len(value) > 32 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') &&
			character != '-' && character != '_' {

			return false
		}
	}
	return true
}

func validStreamRendition(value StreamRenditionData) bool {
	if !validStreamCodec(value.Codec) || len(value.Profile) > 32 {
		return false
	}
	switch value.DynamicRange {
	case StreamDynamicRangeSDR:
		return value.BitDepth == 8 &&
			value.ColorPrimaries == StreamColorPrimariesBT709 &&
			value.Transfer == StreamTransferBT709 &&
			value.Matrix == StreamMatrixBT709
	case StreamDynamicRangeHDR10:
		return validHDRStreamCodecProfile(value.Codec, value.Profile) &&
			value.BitDepth == 10 &&
			value.ColorPrimaries == StreamColorPrimariesBT2020 &&
			value.Transfer == StreamTransferPQ &&
			value.Matrix == StreamMatrixBT2020NCL
	case StreamDynamicRangeHLG:
		return validHDRStreamCodecProfile(value.Codec, value.Profile) &&
			value.BitDepth == 10 &&
			value.ColorPrimaries == StreamColorPrimariesBT2020 &&
			value.Transfer == StreamTransferHLG &&
			value.Matrix == StreamMatrixBT2020NCL
	default:
		return false
	}
}

func validHDRStreamCodecProfile(codec StreamCodec, profile string) bool {
	return (codec == StreamCodecAV1 && profile == "main10") ||
		(codec == StreamCodecVP9 && profile == "2")
}

func onlySDRStreamRenditions(values []StreamRenditionData) bool {
	return len(values) == 1 && values[0].DynamicRange == StreamDynamicRangeSDR
}

func selectStreamRendition(
	values []StreamRenditionData,
	supportedDynamicRanges []StreamDynamicRange,
	codecProfiles []StreamCodecProfileData,
) string {
	supportedRanges := make(map[StreamDynamicRange]struct{}, len(supportedDynamicRanges))
	for _, value := range supportedDynamicRanges {
		if value == StreamDynamicRangeHDR10 || value == StreamDynamicRangeHLG {
			supportedRanges[value] = struct{}{}
		}
	}
	for _, rendition := range values {
		if rendition.DynamicRange == StreamDynamicRangeSDR {
			continue
		}
		if _, supported := supportedRanges[rendition.DynamicRange]; !supported {
			continue
		}
		for _, profile := range codecProfiles {
			if profile.Codec == rendition.Codec && profile.Profile == rendition.Profile {
				return rendition.ID
			}
		}
	}
	for _, rendition := range values {
		if rendition.DynamicRange == StreamDynamicRangeSDR {
			return rendition.ID
		}
	}
	return ""
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

func (h *Hub) RequestStreamP2PRestart(
	client *Client,
	targetConnectionID string,
) error {
	target, senderIsPublisher, ok := h.streams.relationship(
		client,
		targetConnectionID,
	)
	if !ok || senderIsPublisher {
		return ErrStreamP2PRelation
	}
	if !target.enqueue(
		OutgoingEvent{
			Type: EventStreamP2PRestart,
			Data: StreamViewerData{
				ConnectionID: client.ConnectionID(),
				UserID:       client.UserID(),
			},
		},
	) {
		h.Unregister(target)
		return ErrStreamUnavailable
	}
	return nil
}

func (h *Hub) RequestStreamRecovery(
	client *Client,
	serverID int64,
	channelID int64,
	action StreamRecoveryAction,
) error {
	if client == nil || !ValidStreamRecoveryAction(action) {
		return ErrStreamUnavailable
	}
	if _, ok := h.streams.recoveryViewer(client, serverID, channelID); !ok {
		return ErrStreamP2PRelation
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
