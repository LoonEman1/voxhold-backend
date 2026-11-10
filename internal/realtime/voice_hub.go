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
	return h.JoinVoiceWithDevice(client, serverID, channelID, selfMute, selfDeaf, "")
}

// JoinVoiceWithDevice joins like JoinVoice and records the E2EE device the
// client encrypts with; the caller has checked the channel policy.
func (h *Hub) JoinVoiceWithDevice(
	client *Client,
	serverID int64,
	channelID int64,
	selfMute bool,
	selfDeaf bool,
	e2eeDeviceID string,
) (VoiceJoinedData, bool) {
	return h.JoinVoiceWithOptions(client, VoiceJoinData{
		ServerID: serverID, ChannelID: channelID, SelfMute: selfMute, SelfDeaf: selfDeaf, E2EEDeviceID: e2eeDeviceID,
	}, false)
}

func (h *Hub) JoinVoiceWithOptions(client *Client, data VoiceJoinData, serverMute bool) (VoiceJoinedData, bool) {
	if client == nil {
		return VoiceJoinedData{}, false
	}
	defer h.mediaTransitions.Lock(client.UserID())()
	serverID, channelID, e2eeDeviceID := data.ServerID, data.ChannelID, data.E2EEDeviceID
	// Recheck registration inside the same user transition lock used by live
	// revocation. A join validated before a revoke must not appear afterwards.
	if e2eeDeviceID != "" && h.voiceDeviceActive != nil {
		if active, err := h.voiceDeviceActive(client.UserID(), e2eeDeviceID); err != nil || !active {
			return VoiceJoinedData{}, false
		}
	}
	if !client.canAccessChannel(channelID) {
		return VoiceJoinedData{}, false
	}
	if client == nil || serverID <= 0 || channelID <= 0 ||
		!client.hasServer(serverID) {

		return VoiceJoinedData{}, false
	}

	select {
	case <-client.Done():
		return VoiceJoinedData{}, false
	default:
	}

	if current, exists := h.voice.current(client); exists &&
		(current.ServerID != serverID ||
			current.ChannelID != channelID) {

		h.leaveStream(client, "voice channel changed")
	}

	result := h.voice.join(client, data, serverMute)

	if result.previous != nil {
		h.publishVoiceLeft(
			*result.previous,
			client,
		)
	}

	if result.replaced != nil {
		h.leaveStream(
			result.replaced,
			"voice session moved to another connection",
		)
		h.closeVoiceSession(result.replaced.ConnectionID())
		h.SendToConnection(
			result.replaced.ConnectionID(),
			OutgoingEvent{
				Type: EventVoiceWebRTCClosed,
				Data: VoiceWebRTCClosedData{
					Reason: VoiceWebRTCClosedReasonReplaced,
				},
			},
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

func (h *Hub) VoiceTarget(data VoiceModerationData) (*Client, VoiceParticipantData, bool) {
	client := h.voice.userClient(data.UserID)
	if client == nil {
		return nil, VoiceParticipantData{}, false
	}
	participant, ok := h.voice.current(client)
	return client, participant, ok && participant.ConnectionID == data.ConnectionID &&
		participant.ServerID == data.ServerID && participant.ChannelID == data.ChannelID
}

func (h *Hub) CurrentVoice(client *Client) (VoiceParticipantData, bool) {
	return h.voice.current(client)
}

func (h *Hub) SetVoiceServerMute(client *Client, muted bool) {
	defer h.mediaTransitions.Lock(client.UserID())()
	if participant, ok := h.voice.setServerMute(client, muted); ok {
		h.publishToServerExcept(participant.ServerID, nil, OutgoingEvent{Type: EventVoiceStateUpdated, Data: participant})
	}
}

func (h *Hub) DisconnectVoiceParticipant(client *Client, expected VoiceParticipantData, reason string, destination int64) bool {
	defer h.mediaTransitions.Lock(client.UserID())()
	current, active := h.voice.current(client)
	if !active || current.generation != expected.generation {
		return false
	}
	left, ok := h.LeaveVoice(client)
	if !ok {
		return false
	}
	left.Reason = reason
	left.DestinationChannelID = destination
	return h.SendToConnection(client.ConnectionID(), OutgoingEvent{Type: EventVoiceLeft, Data: left})
}

func (h *Hub) UpdateVoiceState(
	client *Client,
	selfMute bool,
	selfDeaf bool,
	selfVideo ...bool,
) (VoiceParticipantData, bool) {
	if client == nil {
		return VoiceParticipantData{}, false
	}

	participant, changed, exists := h.voice.update(
		client,
		selfMute,
		selfDeaf,
		selfVideo...,
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

	h.leaveStream(client, "left voice channel")

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
	if current, exists := h.voice.current(client); exists &&
		current.ServerID == serverID {

		h.leaveStream(client, "server access was revoked")
	}
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

// RelayVoiceE2EE forwards a wrapped sender key (or a request for one) from
// sender to the recipient's connection when both are in the same encrypted
// voice channel. The payload is opaque to the server.
func (h *Hub) RelayVoiceE2EE(sender *Client, eventType EventType, data E2EERelayData) bool {
	from, recipient, ok := h.voice.e2eeRecipient(sender, data.ChannelID, data.RecipientUserID, data.RecipientDeviceID)
	if !ok {
		return false
	}
	data.SenderUserID = from.UserID
	data.SenderDeviceID = from.E2EEDeviceID
	data.SenderConnectionID = from.ConnectionID
	data.RecipientUserID = 0
	data.RecipientDeviceID = ""
	if recipient.enqueue(OutgoingEvent{Type: eventType, Data: data}) {
		return true
	}
	h.Unregister(recipient)
	return false
}

// ResetVoiceChannel disconnects everyone in a voice channel, e.g. after its
// encryption policy changed, so clients rejoin in the new mode.
func (h *Hub) ResetVoiceChannel(channelID int64, reason string) {
	for _, client := range h.voice.channelClients(channelID) {
		if _, left := h.leaveVoice(client); !left {
			continue
		}
		h.SendToConnection(client.ConnectionID(), OutgoingEvent{
			Type: EventVoiceWebRTCClosed,
			Data: VoiceWebRTCClosedData{Reason: reason},
		})
	}
}

// SetVoiceDeviceValidator is configured before accepting connections.
func (h *Hub) SetVoiceDeviceValidator(check func(int64, string) (bool, error)) {
	h.voiceDeviceActive = check
}

// RevalidateVoiceE2EE applies explicit revocation and LRU device eviction to
// the current call as well as future joins. Media packet forwarding stays free
// of database lookups.
func (h *Hub) RevalidateVoiceE2EE(userID int64) {
	defer h.mediaTransitions.Lock(userID)()
	client := h.voice.userClient(userID)
	if client == nil || h.voiceDeviceActive == nil {
		return
	}
	current, ok := h.voice.current(client)
	if !ok || current.E2EEDeviceID == "" {
		return
	}
	if active, err := h.voiceDeviceActive(userID, current.E2EEDeviceID); err == nil && active {
		return
	}
	if _, left := h.leaveVoice(client); left {
		h.SendToConnection(client.ConnectionID(), OutgoingEvent{Type: EventVoiceWebRTCClosed, Data: VoiceWebRTCClosedData{Reason: VoiceWebRTCClosedReasonE2EEDevice}})
	}
}
