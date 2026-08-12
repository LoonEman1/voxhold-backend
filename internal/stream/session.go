package stream

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

type sessionRole uint8

const (
	sessionPublisher sessionRole = iota + 1
	sessionViewer
)

type session struct {
	manager *Manager
	room    *room
	peer    *webrtc.PeerConnection

	connectionID string
	userID       int64
	serverID     int64
	channelID    int64
	role         sessionRole

	closed       atomic.Bool
	videoBitrate *bitrateLimiter
	audioBitrate *bitrateLimiter
	videoSSRC    atomic.Uint32

	negotiationMu      sync.Mutex
	negotiationPending bool
	pendingCandidates  []ICECandidate
	closeOnce          sync.Once
}

func newSession(
	manager *Manager,
	streamRoom *room,
	peer *webrtc.PeerConnection,
	connectionID string,
	userID int64,
	serverID int64,
	channelID int64,
	role sessionRole,
) *session {
	return &session{
		manager:      manager,
		room:         streamRoom,
		peer:         peer,
		connectionID: connectionID,
		userID:       userID,
		serverID:     serverID,
		channelID:    channelID,
		role:         role,
		videoBitrate: newBitrateLimiter(
			manager.maxVideoBitrateKbps,
			2,
		),
		audioBitrate: newBitrateLimiter(
			manager.maxStreamAudioBitrateKbps,
			3,
		),
	}
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
				"stream signaling connection was lost",
			)
		}
	})

	s.peer.OnConnectionStateChange(
		func(state webrtc.PeerConnectionState) {
			if state == webrtc.PeerConnectionStateConnected &&
				s.role == sessionViewer {

				s.room.requestKeyFrame()
			}
			if state == webrtc.PeerConnectionStateFailed ||
				(state == webrtc.PeerConnectionStateClosed &&
					!s.closed.Load()) {

				go s.manager.failSession(
					s,
					"stream WebRTC connection failed",
				)
			}
		},
	)

	if s.role == sessionPublisher {
		s.peer.OnTrack(
			func(
				remote *webrtc.TrackRemote,
				_ *webrtc.RTPReceiver,
			) {
				s.forwardTrack(remote)
			},
		)
	}
}

func (s *session) forwardTrack(remote *webrtc.TrackRemote) {
	if s.closed.Load() ||
		(remote.Kind() != webrtc.RTPCodecTypeVideo &&
			remote.Kind() != webrtc.RTPCodecTypeAudio) {

		return
	}
	if remote.Kind() == webrtc.RTPCodecTypeVideo &&
		codecForMimeType(remote.Codec().MimeType) != s.room.codec {

		s.manager.failSession(s, "publisher negotiated an unexpected stream codec")
		return
	}

	trackName := "screen-video"
	if remote.Kind() == webrtc.RTPCodecTypeAudio {
		trackName = "screen-audio"
	}
	local, err := webrtc.NewTrackLocalStaticRTP(
		remote.Codec().RTPCodecCapability,
		trackName+"-"+s.connectionID,
		"stream-"+strconv.FormatInt(s.channelID, 10),
	)
	if err != nil {
		s.manager.failSession(
			s,
			"failed to create outgoing stream track",
		)
		return
	}

	if !s.room.addTrack(roomTrack{
		kind:  remote.Kind(),
		track: local,
		codec: remote.Codec().RTPCodecCapability,
	}) {
		s.manager.failSession(
			s,
			"duplicate stream media track",
		)
		return
	}
	if remote.Kind() == webrtc.RTPCodecTypeVideo {
		s.videoSSRC.Store(uint32(remote.SSRC()))
		// Ask for the first key frame once. Further key frames are requested
		// when a viewer connects or explicitly reports picture loss.
		s.requestKeyFrame()
	}
	s.room.synchronizeViewers()
	defer func() {
		if remote.Kind() == webrtc.RTPCodecTypeVideo {
			s.videoSSRC.Store(0)
		}
		if s.room.removeTrack(remote.Kind(), local) {
			s.room.synchronizeViewers()
		}
	}()

	limiter := s.videoBitrate
	limitReason := "stream video bitrate limit exceeded"
	if remote.Kind() == webrtc.RTPCodecTypeAudio {
		limiter = s.audioBitrate
		limitReason = "stream audio bitrate limit exceeded"
	}

	for {
		packet, _, err := remote.ReadRTP()
		if err != nil {
			return
		}
		if !limiter.allow(len(packet.Payload)) {
			go s.manager.failSession(s, limitReason)
			return
		}

		packet.Extension = false
		packet.Extensions = nil
		if err := local.WriteRTP(packet); err != nil {
			return
		}
	}
}

func (s *session) createOffer() error {
	s.negotiationMu.Lock()
	defer s.negotiationMu.Unlock()
	if s.closed.Load() {
		return ErrSessionNotFound
	}
	return s.createOfferLocked()
}

func (s *session) synchronizeViewerTracks(force bool) error {
	if s.role != sessionViewer {
		return nil
	}
	tracks := s.room.trackSnapshot()
	// A PeerConnection without media sections produces an SDP answer without
	// ICE credentials in browsers. Wait for the publisher's first RTP track;
	// forwardTrack will call synchronizeViewers again as soon as it arrives.
	if len(tracks) == 0 {
		return nil
	}

	s.negotiationMu.Lock()
	if s.closed.Load() {
		s.negotiationMu.Unlock()
		return ErrSessionNotFound
	}

	desired := make(map[string]roomTrack, len(tracks))
	for _, value := range tracks {
		desired[value.track.ID()] = value
	}
	existing := make(map[string]struct{})
	changed := false

	for _, sender := range s.peer.GetSenders() {
		track := sender.Track()
		if track == nil {
			continue
		}
		if _, exists := desired[track.ID()]; !exists {
			if err := s.peer.RemoveTrack(sender); err != nil {
				s.negotiationMu.Unlock()
				return fmt.Errorf(
					"remove outgoing stream track: %w",
					err,
				)
			}
			changed = true
			continue
		}
		existing[track.ID()] = struct{}{}
	}

	for id, value := range desired {
		if _, exists := existing[id]; exists {
			continue
		}
		sender, err := s.peer.AddTrack(value.track)
		if err != nil {
			s.negotiationMu.Unlock()
			return fmt.Errorf(
				"add outgoing stream track: %w",
				err,
			)
		}
		changed = true
		if value.kind == webrtc.RTPCodecTypeVideo {
			transceiver := transceiverForSender(s.peer, sender)
			if transceiver == nil {
				s.negotiationMu.Unlock()
				return errors.New("outgoing stream video transceiver is missing")
			}
			if err := transceiver.SetCodecPreferences([]webrtc.RTPCodecParameters{{
				RTPCodecCapability: value.codec,
			}}); err != nil {
				s.negotiationMu.Unlock()
				return fmt.Errorf("prefer outgoing %s stream codec: %w", s.room.codec, err)
			}
		}
		go drainViewerRTCP(sender, s.room)
	}

	if !changed && !force {
		s.negotiationMu.Unlock()
		return nil
	}
	if s.peer.SignalingState() != webrtc.SignalingStateStable {
		s.negotiationPending = true
		s.negotiationMu.Unlock()
		return nil
	}

	err := s.createOfferLocked()
	s.negotiationMu.Unlock()
	return err
}

func transceiverForSender(peer *webrtc.PeerConnection, sender *webrtc.RTPSender) *webrtc.RTPTransceiver {
	for _, transceiver := range peer.GetTransceivers() {
		if transceiver.Sender() == sender {
			return transceiver
		}
	}
	return nil
}

func (s *session) createOfferLocked() error {
	offer, err := s.peer.CreateOffer(nil)
	if err == nil {
		err = s.peer.SetLocalDescription(offer)
	}
	if err != nil {
		return fmt.Errorf("create stream WebRTC offer: %w", err)
	}

	s.negotiationPending = false
	if !s.manager.sink.SendOffer(s.connectionID, offer.SDP) {
		go s.manager.failSession(
			s,
			"stream signaling connection was lost",
		)
	}
	return nil
}

func (s *session) acceptAnswer(sdp string) error {
	if sdp == "" {
		return errors.New("stream WebRTC answer SDP is required")
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
	if pending && s.role == sessionViewer {
		return s.synchronizeViewerTracks(false)
	}
	return nil
}

func (s *session) addICECandidate(candidate ICECandidate) error {
	if candidate.Candidate == "" {
		return errors.New("stream ICE candidate is required")
	}

	s.negotiationMu.Lock()
	defer s.negotiationMu.Unlock()
	if s.closed.Load() {
		return ErrSessionNotFound
	}
	if s.peer.RemoteDescription() == nil {
		if len(s.pendingCandidates) >= MaxPendingICECandidates {
			return ErrTooManyICECandidates
		}
		s.pendingCandidates = append(
			s.pendingCandidates,
			candidate,
		)
		return nil
	}
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

func (s *session) requestKeyFrame() {
	ssrc := s.videoSSRC.Load()
	if s.closed.Load() || ssrc == 0 {
		return
	}
	_ = s.peer.WriteRTCP([]rtcp.Packet{
		&rtcp.PictureLossIndication{MediaSSRC: ssrc},
	})
}

func drainViewerRTCP(sender *webrtc.RTPSender, room *room) {
	for {
		packets, _, err := sender.ReadRTCP()
		if err != nil {
			return
		}
		for _, packet := range packets {
			switch packet.(type) {
			case *rtcp.PictureLossIndication,
				*rtcp.FullIntraRequest:

				room.requestKeyFrame()
			}
		}
	}
}
