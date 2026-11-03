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
	replaced     *Client
	joined       bool
	updated      bool
}

type voiceState struct {
	nextGeneration uint64
	mu             sync.RWMutex
	byChannel      map[int64]map[*Client]VoiceParticipantData
	byClient       map[*Client]VoiceParticipantData
	byUser         map[int64]*Client
}

func newVoiceState() *voiceState {
	return &voiceState{
		byChannel: make(
			map[int64]map[*Client]VoiceParticipantData,
		),
		byClient: make(map[*Client]VoiceParticipantData),
		byUser:   make(map[int64]*Client),
	}
}

func (s *voiceState) join(
	client *Client,
	serverID int64,
	channelID int64,
	selfMute bool,
	selfDeaf bool,
	e2eeDeviceID string,
) voiceJoinResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextGeneration++
	result := voiceJoinResult{
		participant: VoiceParticipantData{
			generation:   s.nextGeneration,
			ConnectionID: client.ConnectionID(),
			UserID:       client.UserID(),
			ServerID:     serverID,
			ChannelID:    channelID,
			SelfMute:     selfMute,
			SelfDeaf:     selfDeaf,
			E2EEDeviceID: e2eeDeviceID,
		},
	}

	previous, exists := s.byClient[client]
	if exists &&
		previous.ServerID == serverID &&
		previous.ChannelID == channelID &&
		previous.E2EEDeviceID == e2eeDeviceID {

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

	if previousClient := s.byUser[client.UserID()]; previousClient != nil && previousClient != client {

		previous, exists = s.byClient[previousClient]
		if exists {
			previousCopy := previous
			result.previous = &previousCopy
			result.replaced = previousClient
			s.removeLocked(previousClient, previous)
		}
	}

	room := s.byChannel[channelID]
	if room == nil {
		room = make(map[*Client]VoiceParticipantData)
		s.byChannel[channelID] = room
	}

	room[client] = result.participant
	s.byClient[client] = result.participant
	s.byUser[client.UserID()] = client
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

func (s *voiceState) current(
	client *Client,
) (VoiceParticipantData, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	participant, exists := s.byClient[client]
	return participant, exists
}

func (s *voiceState) userClient(userID int64) *Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byUser[userID]
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

func (s *voiceState) removeChannel(
	channelID int64,
) []VoiceParticipantData {
	s.mu.Lock()
	defer s.mu.Unlock()

	room := s.byChannel[channelID]
	participants := make(
		[]VoiceParticipantData,
		0,
		len(room),
	)

	for client, participant := range room {
		participants = append(participants, participant)
		delete(s.byClient, client)
		if s.byUser[participant.UserID] == client {
			delete(s.byUser, participant.UserID)
		}
	}

	delete(s.byChannel, channelID)
	return participants
}

func (s *voiceState) removeServer(
	serverID int64,
) []VoiceParticipantData {
	s.mu.Lock()
	defer s.mu.Unlock()

	participants := make([]VoiceParticipantData, 0)

	for client, participant := range s.byClient {
		if participant.ServerID == serverID {
			participants = append(participants, participant)
			s.removeLocked(client, participant)
		}
	}

	return participants
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
	if s.byUser[participant.UserID] == client {
		delete(s.byUser, participant.UserID)
	}

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

// e2eeRecipient returns the client of recipientUserID when both it and sender
// are in channelID and the recipient joined with recipientDeviceID.
func (s *voiceState) e2eeRecipient(sender *Client, channelID, recipientUserID int64, recipientDeviceID string) (VoiceParticipantData, *Client, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	from, ok := s.byClient[sender]
	if !ok || from.ChannelID != channelID || from.E2EEDeviceID == "" {
		return VoiceParticipantData{}, nil, false
	}
	recipient := s.byUser[recipientUserID]
	if recipient == nil || recipient == sender {
		return VoiceParticipantData{}, nil, false
	}
	to, ok := s.byClient[recipient]
	if !ok || to.ChannelID != channelID || to.E2EEDeviceID == "" || to.E2EEDeviceID != recipientDeviceID {
		return VoiceParticipantData{}, nil, false
	}
	return from, recipient, true
}

// channelClients lists the clients currently in a voice channel.
func (s *voiceState) channelClients(channelID int64) []*Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	clients := make([]*Client, 0, len(s.byChannel[channelID]))
	for client := range s.byChannel[channelID] {
		clients = append(clients, client)
	}
	return clients
}
