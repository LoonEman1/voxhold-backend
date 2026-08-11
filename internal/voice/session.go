package voice

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/pion/webrtc/v4"
)

type session struct {
	manager *Manager
	room    *room
	peer    *webrtc.PeerConnection

	connectionID string
	userID       int64
	serverID     int64
	channelID    int64

	selfMute atomic.Bool
	selfDeaf atomic.Bool
	closed   atomic.Bool

	negotiationMu      sync.Mutex
	negotiationPending bool
	pendingCandidates  []ICECandidate
	closeOnce          sync.Once
}

func newSession(
	manager *Manager,
	voiceRoom *room,
	peer *webrtc.PeerConnection,
	connectionID string,
	userID int64,
	serverID int64,
	channelID int64,
	selfMute bool,
	selfDeaf bool,
) *session {
	value := &session{
		manager:      manager,
		room:         voiceRoom,
		peer:         peer,
		connectionID: connectionID,
		userID:       userID,
		serverID:     serverID,
		channelID:    channelID,
	}

	value.selfMute.Store(selfMute)
	value.selfDeaf.Store(selfDeaf)

	return value
}

func (s *session) installCallbacks() {
	s.peer.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil || s.closed.Load() {
			return
		}

		value := candidate.ToJSON()
		if !s.manager.sink.SendICECandidate(
			s.connectionID,
			ICECandidate{
				Candidate:        value.Candidate,
				SDPMid:           value.SDPMid,
				SDPMLineIndex:    value.SDPMLineIndex,
				UsernameFragment: value.UsernameFragment,
			},
		) {
			go s.manager.failSession(
				s,
				"WebRTC signaling connection was lost",
			)
		}
	})

	s.peer.OnConnectionStateChange(
		func(state webrtc.PeerConnectionState) {
			switch state {
			case webrtc.PeerConnectionStateFailed:
				go s.manager.failSession(
					s,
					"WebRTC connection failed",
				)

			case webrtc.PeerConnectionStateClosed:
				if !s.closed.Load() {
					go s.manager.failSession(
						s,
						"WebRTC connection closed",
					)
				}

			default:
			}
		},
	)

	s.peer.OnTrack(
		func(
			remote *webrtc.TrackRemote,
			_ *webrtc.RTPReceiver,
		) {
			if remote.Kind() != webrtc.RTPCodecTypeAudio {
				return
			}

			s.forwardAudio(remote)
		},
	)
}

func (s *session) setState(
	selfMute bool,
	selfDeaf bool,
) bool {
	s.selfMute.Store(selfMute)
	return s.selfDeaf.Swap(selfDeaf) != selfDeaf
}

func (s *session) forwardAudio(
	remote *webrtc.TrackRemote,
) {
	if s.closed.Load() {
		return
	}

	trackID := "audio-" + s.connectionID
	streamID := "voice-" + strconv.FormatInt(
		s.channelID,
		10,
	)

	local, err := webrtc.NewTrackLocalStaticRTP(
		remote.Codec().RTPCodecCapability,
		trackID,
		streamID,
	)
	if err != nil {
		s.manager.failSession(
			s,
			"failed to create outgoing audio track",
		)
		return
	}

	if !s.room.addTrack(
		roomTrack{
			ownerConnectionID: s.connectionID,
			track:             local,
		},
	) {
		return
	}

	s.room.synchronizeSessions()

	defer func() {
		if s.room.removeTrack(trackID, s.connectionID) {
			s.room.synchronizeSessions()
		}
	}()

	for {
		packet, _, err := remote.ReadRTP()
		if err != nil {
			return
		}

		if s.selfMute.Load() {
			continue
		}

		packet.Extension = false
		packet.Extensions = nil

		if err := local.WriteRTP(packet); err != nil {
			return
		}
	}
}

func (s *session) synchronizeTracks(forceOffer bool) error {
	tracks := s.room.trackSnapshot()

	s.negotiationMu.Lock()
	if s.closed.Load() {
		s.negotiationMu.Unlock()
		return nil
	}

	desired := make(map[string]roomTrack, len(tracks))
	if !s.selfDeaf.Load() {
		for trackID, track := range tracks {
			if track.ownerConnectionID != s.connectionID {
				desired[trackID] = track
			}
		}
	}

	changed := false
	existing := make(map[string]struct{})

	for _, sender := range s.peer.GetSenders() {
		track := sender.Track()
		if track == nil {
			continue
		}

		trackID := track.ID()
		if _, exists := desired[trackID]; !exists {
			if err := s.peer.RemoveTrack(sender); err != nil {
				s.negotiationMu.Unlock()
				return fmt.Errorf(
					"remove outgoing audio track: %w",
					err,
				)
			}
			changed = true
			continue
		}

		existing[trackID] = struct{}{}
	}

	for trackID, track := range desired {
		if _, exists := existing[trackID]; exists {
			continue
		}

		sender, err := s.peer.AddTrack(track.track)
		if err != nil {
			s.negotiationMu.Unlock()
			return fmt.Errorf(
				"add outgoing audio track: %w",
				err,
			)
		}

		changed = true
		go drainRTCP(sender)
	}

	if !changed && !forceOffer {
		s.negotiationMu.Unlock()
		return nil
	}

	if s.peer.SignalingState() != webrtc.SignalingStateStable {
		s.negotiationPending = true
		s.negotiationMu.Unlock()
		return nil
	}

	offer, err := s.peer.CreateOffer(nil)
	if err == nil {
		err = s.peer.SetLocalDescription(offer)
	}
	if err != nil {
		s.negotiationMu.Unlock()
		return fmt.Errorf("create WebRTC offer: %w", err)
	}

	s.negotiationPending = false
	s.negotiationMu.Unlock()

	if !s.manager.sink.SendOffer(
		s.connectionID,
		offer.SDP,
	) {
		go s.manager.failSession(
			s,
			"WebRTC signaling connection was lost",
		)
	}

	return nil
}

func (s *session) acceptAnswer(sdp string) error {
	if sdp == "" {
		return errors.New("WebRTC answer SDP is required")
	}

	s.negotiationMu.Lock()
	if s.closed.Load() {
		s.negotiationMu.Unlock()
		return ErrSessionNotFound
	}

	err := s.peer.SetRemoteDescription(
		webrtc.SessionDescription{
			Type: webrtc.SDPTypeAnswer,
			SDP:  sdp,
		},
	)
	pending := s.negotiationPending
	candidates := append(
		[]ICECandidate(nil),
		s.pendingCandidates...,
	)
	if err == nil {
		s.negotiationPending = false
		s.pendingCandidates = nil
	}
	s.negotiationMu.Unlock()

	if err != nil {
		return err
	}

	for _, candidate := range candidates {
		if err := s.addICECandidate(candidate); err != nil {
			return err
		}
	}

	if pending {
		return s.synchronizeTracks(false)
	}

	return nil
}

func (s *session) addICECandidate(
	candidate ICECandidate,
) error {
	if candidate.Candidate == "" {
		return errors.New("ICE candidate is required")
	}

	s.negotiationMu.Lock()
	defer s.negotiationMu.Unlock()

	if s.closed.Load() {
		return ErrSessionNotFound
	}

	if s.peer.RemoteDescription() == nil {
		s.pendingCandidates = append(
			s.pendingCandidates,
			candidate,
		)
		return nil
	}

	return s.addICECandidateLocked(candidate)
}

func (s *session) addICECandidateLocked(
	candidate ICECandidate,
) error {
	return s.peer.AddICECandidate(
		webrtc.ICECandidateInit{
			Candidate:        candidate.Candidate,
			SDPMid:           candidate.SDPMid,
			SDPMLineIndex:    candidate.SDPMLineIndex,
			UsernameFragment: candidate.UsernameFragment,
		},
	)
}

func (s *session) close() {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		_ = s.peer.Close()
	})
}

func drainRTCP(sender *webrtc.RTPSender) {
	buffer := make([]byte, 1500)

	for {
		if _, _, err := sender.Read(buffer); err != nil {
			return
		}
	}
}
