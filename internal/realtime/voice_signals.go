package realtime

import "voxhold-backend/internal/voice"

var _ voice.SignalSink = (*VoiceSignalSink)(nil)

type VoiceSignalSink struct {
	hub *Hub
}

func NewVoiceSignalSink(hub *Hub) *VoiceSignalSink {
	return &VoiceSignalSink{hub: hub}
}

// SessionFailureHandler snapshots the logical join, not just its WebSocket ID.
// A delayed failure of an old PeerConnection must not remove a replacement.
func (s *VoiceSignalSink) SessionFailureHandler(connectionID string) func(string) {
	client := s.hub.clientByConnectionID(connectionID)
	if client == nil {
		return func(string) {}
	}
	participant, exists := s.hub.voice.current(client)
	return func(reason string) {
		defer s.hub.mediaTransitions.Lock(client.UserID())()
		current, active := s.hub.voice.current(client)
		if !exists || !active || current.generation != participant.generation {
			return
		}
		s.CloseVoice(connectionID, reason)
	}
}

func (s *VoiceSignalSink) SendOffer(
	connectionID string,
	sdp string,
) bool {
	return s.hub.SendToConnection(
		connectionID,
		OutgoingEvent{
			Type: EventVoiceWebRTCOffer,
			Data: VoiceWebRTCOfferData{SDP: sdp},
		},
	)
}

func (s *VoiceSignalSink) SendICECandidate(
	connectionID string,
	candidate voice.ICECandidate,
) bool {
	return s.hub.SendToConnection(
		connectionID,
		OutgoingEvent{
			Type: EventVoiceICECandidate,
			Data: VoiceICECandidateData{
				Candidate:        candidate.Candidate,
				SDPMid:           candidate.SDPMid,
				SDPMLineIndex:    candidate.SDPMLineIndex,
				UsernameFragment: candidate.UsernameFragment,
			},
		},
	)
}

func (s *VoiceSignalSink) CloseVoice(
	connectionID string,
	reason string,
) {
	client := s.hub.clientByConnectionID(connectionID)
	if client == nil {
		return
	}

	s.hub.LeaveVoice(client)
	s.hub.SendToConnection(
		connectionID,
		OutgoingEvent{
			Type: EventVoiceWebRTCClosed,
			Data: VoiceWebRTCClosedData{
				Reason: reason,
			},
		},
	)
}

func (h *Hub) SendToConnection(
	connectionID string,
	event OutgoingEvent,
) bool {
	client := h.clientByConnectionID(connectionID)
	if client == nil {
		return false
	}

	if client.enqueue(event) {
		return true
	}

	h.Unregister(client)
	return false
}

func (h *Hub) clientByConnectionID(
	connectionID string,
) *Client {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()

	return h.clientsByConnection[connectionID]
}
