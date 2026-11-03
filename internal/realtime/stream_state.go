package realtime

import (
	"cmp"
	"slices"
	"sync"
	"time"
	"voxhold-backend/internal/streamcontract"
)

type activeStream struct {
	serverID   int64
	channelID  int64
	mode       StreamMode
	codec      StreamCodec
	hasAudio   bool
	renditions []StreamRenditionData
	publisher  *Client
	viewers    map[*Client]struct{}
	startedAt  time.Time
}

type streamClientState struct {
	generation  uint64
	stream      *activeStream
	publisher   bool
	renditionID string
}

type streamState struct {
	nextGeneration uint64
	mu             sync.RWMutex
	byChannel      map[int64]map[*Client]*activeStream
	byClient       map[*Client]streamClientState
}

func newStreamState() *streamState {
	return &streamState{
		byChannel: make(map[int64]map[*Client]*activeStream),
		byClient:  make(map[*Client]streamClientState),
	}
}

func (s *streamState) start(
	client *Client,
	participant VoiceParticipantData,
	mode StreamMode,
	codec StreamCodec,
	hasAudio bool,
	renditions []StreamRenditionData,
) (StreamData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.byClient[client].stream != nil {
		return StreamData{}, ErrStreamAlreadyActive
	}
	if !client.SupportsMultipleStreams() && len(s.byChannel[participant.ChannelID]) > 0 {
		return StreamData{}, ErrStreamAlreadyActive
	}
	if len(s.byChannel[participant.ChannelID]) >= streamcontract.MaxChannelPublishers {
		return StreamData{}, ErrStreamPublisherLimit
	}
	value := &activeStream{
		serverID:   participant.ServerID,
		channelID:  participant.ChannelID,
		mode:       mode,
		codec:      codec,
		hasAudio:   hasAudio,
		renditions: cloneStreamRenditions(renditions),
		publisher:  client,
		viewers:    make(map[*Client]struct{}),
		startedAt:  time.Now(),
	}
	if s.byChannel[value.channelID] == nil {
		s.byChannel[value.channelID] = make(map[*Client]*activeStream)
	}
	s.byChannel[value.channelID][client] = value
	s.nextGeneration++
	s.byClient[client] = streamClientState{
		generation: s.nextGeneration,
		stream:     value,
		publisher:  true,
	}
	return streamData(value), nil
}

func (s *streamState) watch(
	client *Client,
	participant VoiceParticipantData,
	maxServerViewers int,
	maxP2PViewers int,
	supportedDynamicRanges []StreamDynamicRange,
	codecProfiles []StreamCodecProfileData,
	publisherConnectionID string,
) (StreamData, *Client, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var value *activeStream
	for _, candidate := range s.byChannel[participant.ChannelID] {
		if publisherConnectionID != "" && candidate.publisher.ConnectionID() != publisherConnectionID {
			continue
		}
		if value == nil || candidate.startedAt.UnixMilli() < value.startedAt.UnixMilli() || (candidate.startedAt.UnixMilli() == value.startedAt.UnixMilli() && candidate.publisher.ConnectionID() < value.publisher.ConnectionID()) {
			value = candidate
		}
	}
	if value == nil || value.publisher == client ||
		value.serverID != participant.ServerID ||
		s.byClient[client].stream != nil {

		return StreamData{}, nil, "", ErrStreamUnavailable
	}
	maximum := maxServerViewers
	if value.mode == StreamModeP2P {
		maximum = maxP2PViewers
	}
	if len(value.viewers) >= maximum {
		return StreamData{}, nil, "", ErrStreamViewerLimit
	}
	selectedRenditionID := selectStreamRendition(
		value.renditions,
		supportedDynamicRanges,
		codecProfiles,
	)
	if selectedRenditionID == "" {
		return StreamData{}, nil, "", ErrStreamRenditionUnavailable
	}
	value.viewers[client] = struct{}{}
	s.nextGeneration++
	s.byClient[client] = streamClientState{
		generation:  s.nextGeneration,
		stream:      value,
		renditionID: selectedRenditionID,
	}
	return streamData(value), value.publisher, selectedRenditionID, nil
}

func (s *streamState) leave(
	client *Client,
) (*activeStream, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.byClient[client]
	if !exists {
		return nil, false, false
	}
	value := state.stream
	if state.publisher {
		delete(s.byChannel[value.channelID], value.publisher)
		if len(s.byChannel[value.channelID]) == 0 {
			delete(s.byChannel, value.channelID)
		}
		delete(s.byClient, value.publisher)
		for viewer := range value.viewers {
			delete(s.byClient, viewer)
		}
		return value, true, true
	}
	delete(value.viewers, client)
	delete(s.byClient, client)
	return value, false, true
}

func (s *streamState) relationship(
	from *Client,
	targetConnectionID string,
) (*Client, bool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	fromState, exists := s.byClient[from]
	if !exists || fromState.stream.mode != StreamModeP2P {
		return nil, false, false
	}
	value := fromState.stream
	if fromState.publisher {
		for viewer := range value.viewers {
			if viewer.ConnectionID() == targetConnectionID {
				return viewer, true, true
			}
		}
		return nil, true, false
	}
	if value.publisher.ConnectionID() == targetConnectionID {
		return value.publisher, false, true
	}
	return nil, false, false
}

func (s *streamState) snapshotForServers(
	serverIDs []int64,
) []StreamData {
	allowed := make(map[int64]struct{}, len(serverIDs))
	for _, serverID := range serverIDs {
		allowed[serverID] = struct{}{}
	}

	s.mu.RLock()
	result := make([]StreamData, 0, len(s.byChannel))
	for _, streams := range s.byChannel {
		for _, value := range streams {
			if _, exists := allowed[value.serverID]; exists {
				result = append(result, streamData(value))
			}
		}
	}
	s.mu.RUnlock()
	slices.SortFunc(result, func(a, b StreamData) int {
		if order := cmp.Compare(a.ServerID, b.ServerID); order != 0 {
			return order
		}
		if order := cmp.Compare(a.ChannelID, b.ChannelID); order != 0 {
			return order
		}
		if order := cmp.Compare(a.StartedAtMS, b.StartedAtMS); order != 0 {
			return order
		}
		return cmp.Compare(a.PublisherConnectionID, b.PublisherConnectionID)
	})
	return result
}

func (s *streamState) data(value *activeStream) StreamData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return streamData(value)
}

// recoveryViewer returns the active server-mode stream that the client
// currently watches. Publishers and P2P participants never qualify so that
// recovery requests cannot reach a foreign room.
func (s *streamState) recoveryViewer(
	client *Client,
	serverID int64,
	channelID int64,
) (*activeStream, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, exists := s.byClient[client]
	if !exists || state.publisher {
		return nil, false
	}
	value := state.stream
	if value.mode != StreamModeServer ||
		value.serverID != serverID ||
		value.channelID != channelID {

		return nil, false
	}
	return value, true
}

func (s *streamState) isPublisher(client *Client) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byClient[client].publisher
}

func (s *streamState) publishersForChannel(
	channelID int64,
) []*Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Client, 0, len(s.byChannel[channelID]))
	for publisher := range s.byChannel[channelID] {
		result = append(result, publisher)
	}
	return result
}

func streamData(value *activeStream) StreamData {
	return StreamData{
		ServerID:              value.serverID,
		ChannelID:             value.channelID,
		PublisherUserID:       value.publisher.UserID(),
		PublisherConnectionID: value.publisher.ConnectionID(),
		Mode:                  value.mode,
		Codec:                 value.codec,
		HasAudio:              value.hasAudio,
		ViewerCount:           len(value.viewers),
		Renditions:            cloneStreamRenditions(value.renditions),
		StartedAtMS:           value.startedAt.UnixMilli(),
	}
}

func cloneStreamRenditions(values []StreamRenditionData) []StreamRenditionData {
	return append([]StreamRenditionData(nil), values...)
}
