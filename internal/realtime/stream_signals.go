package realtime

import "voxhold-backend/internal/stream"

var _ stream.SignalSink = (*StreamSignalSink)(nil)

type StreamSignalSink struct {
	hub *Hub
}

func NewStreamSignalSink(hub *Hub) *StreamSignalSink {
	return &StreamSignalSink{hub: hub}
}

// Each publisher start and viewer watch has a distinct logical generation.
func (s *StreamSignalSink) SessionFailureHandler(connectionID string) func(string) {
	client := s.hub.clientByConnectionID(connectionID)
	if client == nil {
		return func(string) {}
	}
	s.hub.streams.mu.RLock()
	state, exists := s.hub.streams.byClient[client]
	s.hub.streams.mu.RUnlock()
	return func(reason string) {
		defer s.hub.mediaTransitions.Lock(client.UserID())()
		s.hub.streams.mu.RLock()
		current, active := s.hub.streams.byClient[client]
		s.hub.streams.mu.RUnlock()
		if !exists || !active || current.generation != state.generation {
			return
		}
		s.CloseStream(connectionID, reason)
	}
}

func (s *StreamSignalSink) SendOffer(
	connectionID string,
	sdp string,
) bool {
	return s.hub.SendToConnection(
		connectionID,
		OutgoingEvent{
			Type: EventStreamWebRTCOffer,
			Data: StreamWebRTCOfferData{SDP: sdp},
		},
	)
}

func (s *StreamSignalSink) SendICECandidate(
	connectionID string,
	candidate stream.ICECandidate,
) bool {
	return s.hub.SendToConnection(
		connectionID,
		OutgoingEvent{
			Type: EventStreamICECandidate,
			Data: StreamICECandidateData{
				Candidate:        candidate.Candidate,
				SDPMid:           candidate.SDPMid,
				SDPMLineIndex:    candidate.SDPMLineIndex,
				UsernameFragment: candidate.UsernameFragment,
			},
		},
	)
}

func (s *StreamSignalSink) CloseStream(
	connectionID string,
	reason string,
) {
	client := s.hub.clientByConnectionID(connectionID)
	if client == nil {
		return
	}
	s.hub.LeaveStream(client, reason)
	s.hub.SendToConnection(
		connectionID,
		OutgoingEvent{
			Type: EventStreamWebRTCClosed,
			Data: StreamWebRTCClosedData{
				Reason: reason,
			},
		},
	)
}
