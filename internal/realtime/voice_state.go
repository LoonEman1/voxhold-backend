package realtime

import (
	"cmp"
	"slices"
	"sync"
)

type voiceJoinResult struct {
	participant  VoiceParticipantData
	participants []VoiceParticipantData
	previous     *VoiceParticipantData
	joined       bool
	updated      bool
}

type voiceState struct {
	mu        sync.RWMutex
	byChannel map[int64]map[*Client]VoiceParticipantData
	byClient  map[*Client]VoiceParticipantData
}

func newVoiceState() *voiceState {
	return &voiceState{
		byChannel: make(
			map[int64]map[*Client]VoiceParticipantData,
		),
		byClient: make(map[*Client]VoiceParticipantData),
	}
}

func (s *voiceState) join(
	client *Client,
	serverID int64,
	channelID int64,
	selfMute bool,
	selfDeaf bool,
) voiceJoinResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := voiceJoinResult{
		participant: VoiceParticipantData{
			ConnectionID: client.ConnectionID(),
			UserID:       client.UserID(),
			ServerID:     serverID,
			ChannelID:    channelID,
			SelfMute:     selfMute,
			SelfDeaf:     selfDeaf,
		},
	}

	previous, exists := s.byClient[client]
	if exists &&
		previous.ServerID == serverID &&
		previous.ChannelID == channelID {

		result.updated =
			previous.SelfMute != selfMute ||
				previous.SelfDeaf != selfDeaf

		s.byClient[client] = result.participant
		s.byChannel[channelID][client] = result.participant
		result.participants = participantMapSnapshot(
			s.byChannel[channelID],
		)

		return result
	}

	if exists {
		previousCopy := previous
		result.previous = &previousCopy
		s.removeLocked(client, previous)
	}

	room := s.byChannel[channelID]
	if room == nil {
		room = make(map[*Client]VoiceParticipantData)
		s.byChannel[channelID] = room
	}

	room[client] = result.participant
	s.byClient[client] = result.participant
	result.joined = true
	result.participants = participantMapSnapshot(room)

	return result
}

func (s *voiceState) update(
	client *Client,
	selfMute bool,
	selfDeaf bool,
) (VoiceParticipantData, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	participant, exists := s.byClient[client]
	if !exists {
		return VoiceParticipantData{}, false, false
	}

	changed := participant.SelfMute != selfMute ||
		participant.SelfDeaf != selfDeaf

	participant.SelfMute = selfMute
	participant.SelfDeaf = selfDeaf

	s.byClient[client] = participant
	s.byChannel[participant.ChannelID][client] = participant

	return participant, changed, true
}

func (s *voiceState) leave(
	client *Client,
) (VoiceParticipantData, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	participant, exists := s.byClient[client]
	if !exists {
		return VoiceParticipantData{}, false
	}

	s.removeLocked(client, participant)

	return participant, true
}

func (s *voiceState) leaveServer(
	client *Client,
	serverID int64,
) (VoiceParticipantData, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	participant, exists := s.byClient[client]
	if !exists || participant.ServerID != serverID {
		return VoiceParticipantData{}, false
	}

	s.removeLocked(client, participant)

	return participant, true
}

func (s *voiceState) removeChannel(channelID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	room := s.byChannel[channelID]
	for client := range room {
		delete(s.byClient, client)
	}

	delete(s.byChannel, channelID)
}

func (s *voiceState) removeServer(serverID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for client, participant := range s.byClient {
		if participant.ServerID == serverID {
			s.removeLocked(client, participant)
		}
	}
}

func (s *voiceState) snapshotForServers(
	serverIDs []int64,
) []VoiceParticipantData {
	serverSet := make(map[int64]struct{}, len(serverIDs))
	for _, serverID := range serverIDs {
		serverSet[serverID] = struct{}{}
	}

	s.mu.RLock()
	participants := make(
		[]VoiceParticipantData,
		0,
		len(s.byClient),
	)

	for _, participant := range s.byClient {
		if _, exists := serverSet[participant.ServerID]; exists {
			participants = append(participants, participant)
		}
	}
	s.mu.RUnlock()

	sortVoiceParticipants(participants)

	return participants
}

func (s *voiceState) removeLocked(
	client *Client,
	participant VoiceParticipantData,
) {
	delete(s.byClient, client)

	room := s.byChannel[participant.ChannelID]
	delete(room, client)

	if len(room) == 0 {
		delete(s.byChannel, participant.ChannelID)
	}
}

func participantMapSnapshot(
	room map[*Client]VoiceParticipantData,
) []VoiceParticipantData {
	participants := make(
		[]VoiceParticipantData,
		0,
		len(room),
	)

	for _, participant := range room {
		participants = append(participants, participant)
	}

	sortVoiceParticipants(participants)

	return participants
}

func sortVoiceParticipants(
	participants []VoiceParticipantData,
) {
	slices.SortFunc(
		participants,
		func(a VoiceParticipantData, b VoiceParticipantData) int {
			if result := cmp.Compare(a.ServerID, b.ServerID); result != 0 {
				return result
			}
			if result := cmp.Compare(a.ChannelID, b.ChannelID); result != 0 {
				return result
			}
			if result := cmp.Compare(a.UserID, b.UserID); result != 0 {
				return result
			}

			return cmp.Compare(a.ConnectionID, b.ConnectionID)
		},
	)
}
