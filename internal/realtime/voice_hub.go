package realtime

type VoiceSessionCloser interface {
	Leave(connectionID string)
}

func (h *Hub) SetVoiceSessionCloser(
	closer VoiceSessionCloser,
) {
	h.voiceSessions = closer
}

func (h *Hub) JoinVoice(
	client *Client,
	serverID int64,
	channelID int64,
	selfMute bool,
	selfDeaf bool,
) (VoiceJoinedData, bool) {
	if client == nil || serverID <= 0 || channelID <= 0 ||
		!client.hasServer(serverID) {

		return VoiceJoinedData{}, false
	}

	select {
	case <-client.Done():
		return VoiceJoinedData{}, false
	default:
	}

	result := h.voice.join(
		client,
		serverID,
		channelID,
		selfMute,
		selfDeaf,
	)

	if result.previous != nil {
		h.publishVoiceLeft(
			*result.previous,
			client,
		)
	}

	if result.joined {
		h.publishToServerExcept(
			serverID,
			client,
			OutgoingEvent{
				Type: EventVoiceParticipantJoined,
				Data: result.participant,
			},
		)
	} else if result.updated {
		h.publishToServerExcept(
			serverID,
			client,
			OutgoingEvent{
				Type: EventVoiceStateUpdated,
				Data: result.participant,
			},
		)
	}

	return VoiceJoinedData{
		Participant:  result.participant,
		Participants: result.participants,
	}, true
}

func (h *Hub) UpdateVoiceState(
	client *Client,
	selfMute bool,
	selfDeaf bool,
) (VoiceParticipantData, bool) {
	if client == nil {
		return VoiceParticipantData{}, false
	}

	participant, changed, exists := h.voice.update(
		client,
		selfMute,
		selfDeaf,
	)
	if !exists {
		return VoiceParticipantData{}, false
	}

	if changed {
		h.publishToServerExcept(
			participant.ServerID,
			client,
			OutgoingEvent{
				Type: EventVoiceStateUpdated,
				Data: participant,
			},
		)
	}

	return participant, true
}

func (h *Hub) LeaveVoice(
	client *Client,
) (VoiceLeftData, bool) {
	participant, exists := h.leaveVoice(client)
	if !exists {
		return VoiceLeftData{}, false
	}

	return newVoiceLeftData(participant), true
}

func (h *Hub) leaveVoice(
	client *Client,
) (VoiceParticipantData, bool) {
	if client == nil {
		return VoiceParticipantData{}, false
	}

	participant, exists := h.voice.leave(client)
	if !exists {
		return VoiceParticipantData{}, false
	}

	h.publishVoiceLeft(participant, client)
	h.closeVoiceSession(participant.ConnectionID)

	return participant, true
}

func (h *Hub) leaveVoiceForServer(
	client *Client,
	serverID int64,
) {
	participant, exists := h.voice.leaveServer(
		client,
		serverID,
	)
	if exists {
		h.publishVoiceLeft(participant, client)
		h.closeVoiceSession(participant.ConnectionID)
	}
}

func (h *Hub) closeVoiceSession(connectionID string) {
	if h.voiceSessions != nil {
		h.voiceSessions.Leave(connectionID)
	}
}

func (h *Hub) publishVoiceLeft(
	participant VoiceParticipantData,
	excluded *Client,
) {
	h.publishToServerExcept(
		participant.ServerID,
		excluded,
		OutgoingEvent{
			Type: EventVoiceParticipantLeft,
			Data: newVoiceLeftData(participant),
		},
	)
}

func (h *Hub) sendVoiceSnapshot(client *Client) {
	participants := h.voice.snapshotForServers(
		client.serverIDs(),
	)

	if !client.enqueue(
		OutgoingEvent{
			Type: EventVoiceSnapshot,
			Data: VoiceSnapshotData{
				Participants: participants,
			},
		},
	) {
		h.Unregister(client)
	}
}

func (h *Hub) publishToServerExcept(
	serverID int64,
	excluded *Client,
	event OutgoingEvent,
) int {
	clients := h.clientsForServer(serverID)
	filtered := make([]*Client, 0, len(clients))

	for _, client := range clients {
		if client != excluded {
			filtered = append(filtered, client)
		}
	}

	return h.broadcastToClients(filtered, event)
}

func newVoiceLeftData(
	participant VoiceParticipantData,
) VoiceLeftData {
	return VoiceLeftData{
		ConnectionID: participant.ConnectionID,
		UserID:       participant.UserID,
		ServerID:     participant.ServerID,
		ChannelID:    participant.ChannelID,
	}
}
