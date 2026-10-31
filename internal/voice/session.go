package voice

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"voxhold-backend/internal/mediahandshake"
	"voxhold-backend/internal/safego"
	"voxhold-backend/internal/telemetry"
	"voxhold-backend/internal/webrtcrecovery"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

type session struct {
	onFailure func(string)
	manager   *Manager
	room      *room
	peer      *webrtc.PeerConnection

	connectionID string
	userID       int64
	serverID     int64
	channelID    int64

	selfMute     atomic.Bool
	selfDeaf     atomic.Bool
	closed       atomic.Bool
	audioBitrate *audioBitrateLimiter

	negotiationMu      sync.Mutex
	offerStarted       time.Time
	negotiationPending bool
	iceRestartPending  bool
	pendingCandidates  []ICECandidate
	closeOnce          sync.Once
	handshake          *mediahandshake.Deadline
	recovery           *webrtcrecovery.Controller
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
		audioBitrate: newAudioBitrateLimiter(
			manager.maxAudioBitrateKbps,
		),
		recovery: webrtcrecovery.New(
			webrtcrecovery.Policy{
				InitialDelay: 2 * time.Second,
				RetryDelay:   8 * time.Second,
				MaxAttempts:  4,
			},
		),
	}

	// Capture the logical join now; failures from this PeerConnection must
	// never tear down a later join on the same WebSocket connection.
	value.onFailure = func(reason string) { manager.sink.CloseVoice(connectionID, reason) }
	if sink, ok := manager.sink.(interface{ SessionFailureHandler(string) func(string) }); ok {
		value.onFailure = sink.SessionFailureHandler(connectionID)
	}
	value.selfMute.Store(selfMute)
	value.selfDeaf.Store(selfDeaf)

	value.handshake = mediahandshake.New(func(reason string) { manager.failSession(value, reason) })
	return value
}

func (s *session) installCallbacks() {
	s.peer.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		safego.Run("voice/onICECandidate", func() {
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
				s.scheduleFail("WebRTC signaling connection was lost")
			}
		})
	})

	s.peer.OnConnectionStateChange(
		func(state webrtc.PeerConnectionState) {
			// A panic inside the state handler would otherwise escape into
			// Pion's callback goroutine and take down the whole process;
			// contain it and fail only this damaged session.
			safego.GuardedRun("voice/onConnectionStateChange", func() {
				s.handleConnectionState(state)
			}, func() {
				s.scheduleFail("internal error in connection state handler")
			})
		},
	)

	s.peer.OnTrack(
		func(
			remote *webrtc.TrackRemote,
			_ *webrtc.RTPReceiver,
		) {
			safego.GuardedRun("voice/onTrack", func() {
				if remote.Kind() != webrtc.RTPCodecTypeAudio {
					return
				}

				s.forwardAudio(remote)
			}, func() {
				s.scheduleFail("internal error while forwarding audio")
			})
		},
	)
}

// scheduleFail tears the session down behind a panic boundary so a failure
// path can never itself crash the process.
func (s *session) scheduleFail(reason string) {
	safego.Go("voice/failSession", func() {
		s.manager.failSession(s, reason)
	})
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

	// Pion interceptor v0.1.49 NACK uses PacketFactoryCopy: NewPacket clones
	// the header and copies payload before WriteRTP returns. Static RTP writes
	// are synchronous, so the receive buffer can be reused after each write.
	buffer := make([]byte, 1500)
	var packet rtp.Packet
	for {
		n, _, err := remote.Read(buffer)
		if err != nil {
			return
		}
		packet = rtp.Packet{}
		if err := packet.Unmarshal(buffer[:n]); err != nil {
			continue
		}

		if !s.audioBitrate.allow(len(packet.Payload)) {
			s.scheduleFail("audio bitrate limit exceeded")
			return
		}

		if s.selfMute.Load() {
			continue
		}

		packet.Extension = false
		packet.Extensions = nil

		// TrackLocalStaticRTP still delivers the packet to every other
		// listener when one binding fails (closed DTLS, transient ENOBUFS).
		// Leaving the loop would silence this speaker for the whole channel,
		// so only a failed read from the speaker stops forwarding.
		_ = telemetry.Forward(telemetry.VoiceRTP, func() error { return local.WriteRTP(&packet) })
	}
}

func (s *session) synchronizeTracks(forceOffer bool) error {
	s.negotiationMu.Lock()
	if s.closed.Load() {
		s.negotiationMu.Unlock()
		return nil
	}
	tracks := s.room.trackSnapshot()

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
		safego.Go("voice/drainRTCP", func() {
			drainRTCP(sender)
		})
	}

	if !changed && !forceOffer && !s.negotiationPending && !s.iceRestartPending {
		s.negotiationMu.Unlock()
		return nil
	}

	if s.peer.SignalingState() != webrtc.SignalingStateStable {
		s.negotiationPending = true
		s.negotiationMu.Unlock()
		return nil
	}

	sdp, err := s.createOfferLocked(s.iceRestartPending)
	s.negotiationMu.Unlock()
	if err != nil {
		return err
	}
	s.sendOffer(sdp)
	return nil
}

func (s *session) createOfferLocked(restartICE bool) (string, error) {
	var options *webrtc.OfferOptions
	if restartICE {
		options = &webrtc.OfferOptions{ICERestart: true}
	}
	offer, err := s.peer.CreateOffer(options)
	if err == nil {
		err = s.peer.SetLocalDescription(offer)
	}
	if err != nil {
		return "", fmt.Errorf("create WebRTC offer: %w", err)
	}

	s.negotiationPending = false
	s.iceRestartPending = false
	s.offerStarted = time.Now()
	s.handshake.Offer()
	return offer.SDP, nil
}

func (s *session) sendOffer(sdp string) {
	if !s.manager.sink.SendOffer(
		s.connectionID,
		sdp,
	) {
		s.scheduleFail("WebRTC signaling connection was lost")
	}
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
	restartPending := s.iceRestartPending
	candidates := append(
		[]ICECandidate(nil),
		s.pendingCandidates...,
	)
	if err == nil {
		s.pendingCandidates = nil
		if !s.offerStarted.IsZero() {
			telemetry.Observe(telemetry.VoiceAnswer, time.Since(s.offerStarted), false)
			s.offerStarted = time.Time{}
		}
		s.handshake.Answer()
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

	if pending || restartPending {
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

	if s.peer.RemoteDescription() == nil ||
		s.peer.SignalingState() != webrtc.SignalingStateStable {
		if len(s.pendingCandidates) >= MaxPendingICECandidates {
			return ErrTooManyICECandidates
		}

		s.pendingCandidates = append(
			s.pendingCandidates,
			candidate,
		)
		return nil
	}
	if !candidateMatchesRemoteDescription(
		s.peer.RemoteDescription().SDP,
		candidate.UsernameFragment,
	) {
		return nil
	}

	return s.addICECandidateLocked(candidate)
}

func candidateMatchesRemoteDescription(
	sdp string,
	usernameFragment *string,
) bool {
	if usernameFragment == nil || strings.TrimSpace(*usernameFragment) == "" {
		return true
	}
	const prefix = "a=ice-ufrag:"
	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) &&
			strings.TrimSpace(strings.TrimPrefix(line, prefix)) ==
				strings.TrimSpace(*usernameFragment) {

			return true
		}
	}
	return false
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
		s.handshake.Stop()
		s.recovery.Stop()
		_ = s.peer.Close()
	})
}

func (s *session) handleConnectionState(
	state webrtc.PeerConnectionState,
) {
	switch state {
	case webrtc.PeerConnectionStateConnected:
		s.handshake.Stop()
		s.recovery.Stop()

	case webrtc.PeerConnectionStateDisconnected:
		s.startRecovery(false)

	case webrtc.PeerConnectionStateFailed:
		s.startRecovery(true)

	case webrtc.PeerConnectionStateClosed:
		if !s.closed.Load() {
			s.scheduleFail("WebRTC connection closed")
		}

	default:
	}
}

func (s *session) startRecovery(immediate bool) {
	if s.closed.Load() {
		return
	}
	s.recovery.Start(
		immediate,
		s.restartICE,
		func() {
			if s.closed.Load() ||
				s.peer.ConnectionState() == webrtc.PeerConnectionStateConnected {

				return
			}
			s.manager.failSession(
				s,
				"WebRTC recovery exhausted",
			)
		},
	)
}

func (s *session) restartICE() error {
	s.negotiationMu.Lock()
	if s.closed.Load() {
		s.negotiationMu.Unlock()
		return ErrSessionNotFound
	}
	if s.peer.SignalingState() != webrtc.SignalingStateStable {
		s.iceRestartPending = true
		s.negotiationPending = true
		s.negotiationMu.Unlock()
		return nil
	}
	sdp, err := s.createOfferLocked(true)
	s.negotiationMu.Unlock()
	if err != nil {
		return err
	}
	s.sendOffer(sdp)
	return nil
}

func drainRTCP(sender *webrtc.RTPSender) {
	buffer := make([]byte, 1500)

	for {
		if _, _, err := sender.Read(buffer); err != nil {
			return
		}
	}
}
