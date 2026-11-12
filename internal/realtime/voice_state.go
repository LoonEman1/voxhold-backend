package realtime

import (
	"cmp"
	"slices"
	"sync"
	"time"
)

type voiceJoinResult struct {
	participant  VoiceParticipantData
	participants []VoiceParticipantData
	previous     *VoiceParticipantData
	replaced     *Client
	resumed      bool
	joined       bool
	updated      bool
}

type voiceState struct {
	nextGeneration uint64
	mu             sync.RWMutex
	byChannel      map[int64]map[*Client]VoiceParticipantData
	byClient       map[*Client]VoiceParticipantData
	byUser         map[int64]*Client
	retained       map[*Client]*time.Timer
}

func newVoiceState() *voiceState {
	return &voiceState{
		byChannel: make(
			map[int64]map[*Client]VoiceParticipantData,
		),
		byClient: make(map[*Client]VoiceParticipantData),
		byUser:   make(map[int64]*Client),
		retained: make(map[*Client]*time.Timer),
	}
}

func (s *voiceState) join(
	client *Client,
	data VoiceJoinData,
	serverMute bool,
) voiceJoinResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextGeneration++
	result := voiceJoinResult{
		participant: VoiceParticipantData{
			generation:   s.nextGeneration,
			ConnectionID: client.ConnectionID(),
			UserID:       client.UserID(),
			ServerID:     data.ServerID,
			ChannelID:    data.ChannelID,
			SelfMute:     data.SelfMute,
			SelfDeaf:     data.SelfDeaf,
			ServerMute:   serverMute,
			SupportsMove: data.SupportsMove,
			E2EEDeviceID: data.E2EEDeviceID,
		},
	}

	previous, exists := s.byClient[client]
	channelID := data.ChannelID
	if exists &&
		previous.ServerID == data.ServerID &&
		previous.ChannelID == channelID &&
		previous.E2EEDeviceID == data.E2EEDeviceID {

		result.updated =
			previous.SelfMute != data.SelfMute || previous.SelfDeaf != data.SelfDeaf ||
				previous.ServerMute != serverMute || previous.SupportsMove != data.SupportsMove ||
				previous.Reconnecting
		result.participant.SelfVideo = previous.SelfVideo
		result.participant.Reconnecting = previous.Reconnecting

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
			result.resumed = previous.ServerID == data.ServerID && previous.ChannelID == channelID &&
				previousClient.sessionKey == client.sessionKey &&
				(s.retained[previousClient] != nil || previous.ConnectionID == data.ResumeConnectionID)
			result.participant.Reconnecting = result.resumed
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
	result.joined = !result.resumed
	result.updated = result.resumed
	result.participants = participantMapSnapshot(room)

	return result
}

func (s *voiceState) retain(client *Client, grace time.Duration, expire func(uint64)) (VoiceParticipantData, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	participant, exists := s.byClient[client]
	if exists {
		participant.Reconnecting = true
		s.byClient[client] = participant
		s.byChannel[participant.ChannelID][client] = participant
		s.retained[client] = time.AfterFunc(grace, func() { expire(participant.generation) })
	}
	return participant, exists
}

func (s *voiceState) retainedClients(serverID int64) []*Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	clients := make([]*Client, 0, len(s.retained))
	for client := range s.retained {
		if serverID == 0 || s.byClient[client].ServerID == serverID {
			clients = append(clients, client)
		}
	}
	return clients
}

func (s *voiceState) isRetained(client *Client) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.retained[client] != nil
}

func (s *voiceState) setReconnecting(client *Client, generation uint64, reconnecting bool) (VoiceParticipantData, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	participant, exists := s.byClient[client]
	if !exists || participant.generation != generation || participant.Reconnecting == reconnecting || s.retained[client] != nil {
		return VoiceParticipantData{}, false
	}
	participant.Reconnecting = reconnecting
	s.byClient[client] = participant
	s.byChannel[participant.ChannelID][client] = participant
	return participant, true
}

func (s *voiceState) setServerMute(client *Client, muted bool) (VoiceParticipantData, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	participant, exists := s.byClient[client]
	if exists {
		participant.ServerMute = muted
		s.byClient[client] = participant
		s.byChannel[participant.ChannelID][client] = participant
	}
	return participant, exists
}

func (s *voiceState) update(
	client *Client,
	selfMute bool,
	selfDeaf bool,
	selfVideo ...bool,
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
	if len(selfVideo) > 0 {
		changed = changed || participant.SelfVideo != selfVideo[0]
		participant.SelfVideo = selfVideo[0]
	}

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
		s.removeLocked(client, participant)
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
	if timer := s.retained[client]; timer != nil {
		timer.Stop()
		delete(s.retained, client)
	}
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
	if !ok || s.retained[recipient] != nil || to.ChannelID != channelID || to.E2EEDeviceID == "" || to.E2EEDeviceID != recipientDeviceID {
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
