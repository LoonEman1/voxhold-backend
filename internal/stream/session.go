package stream

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"voxhold-backend/internal/safego"
	"voxhold-backend/internal/webrtcrecovery"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

const colorSpaceRTPHeaderExtensionURI = "http://www.webrtc.org/experiments/rtp-hdrext/color-space"

var (
	streamRecoveryStartedTotal   atomic.Uint64
	streamRecoverySucceededTotal atomic.Uint64
	streamRecoveryExhaustedTotal atomic.Uint64
)

type relayBinding struct {
	id                    string
	ssrc                  webrtc.SSRC
	payloadType           webrtc.PayloadType
	writeStream           webrtc.TrackLocalWriter
	colorSpaceExtensionID uint8
}

// relayTrack deliberately rebuilds outbound RTP headers per PeerConnection.
// Incoming extension IDs are scoped to the publisher SDP and must never be
// copied into a viewer SDP where the same URI may use a different ID.
type relayTrack struct {
	mu        sync.RWMutex
	bindings  []relayBinding
	codec     webrtc.RTPCodecCapability
	id        string
	streamID  string
	colorData []byte
}

func newRelayTrack(
	codec webrtc.RTPCodecCapability,
	id string,
	streamID string,
	rendition Rendition,
) *relayTrack {
	return &relayTrack{
		codec: codec, id: id, streamID: streamID,
		colorData: colorSpaceExtensionData(rendition),
	}
}

func (t *relayTrack) Bind(context webrtc.TrackLocalContext) (webrtc.RTPCodecParameters, error) {
	for _, codec := range context.CodecParameters() {
		if !strings.EqualFold(codec.MimeType, t.codec.MimeType) ||
			codec.ClockRate != t.codec.ClockRate ||
			codec.Channels != t.codec.Channels ||
			!strings.EqualFold(codec.SDPFmtpLine, t.codec.SDPFmtpLine) {

			continue
		}
		var colorSpaceExtensionID uint8
		for _, extension := range context.HeaderExtensions() {
			if extension.ID > 0 && extension.ID <= 255 &&
				strings.EqualFold(extension.URI, colorSpaceRTPHeaderExtensionURI) {

				colorSpaceExtensionID = uint8(extension.ID)
				break
			}
		}
		t.mu.Lock()
		t.bindings = append(t.bindings, relayBinding{
			id: context.ID(), ssrc: context.SSRC(), payloadType: codec.PayloadType,
			writeStream: context.WriteStream(), colorSpaceExtensionID: colorSpaceExtensionID,
		})
		t.mu.Unlock()
		return codec, nil
	}
	return webrtc.RTPCodecParameters{}, webrtc.ErrUnsupportedCodec
}

func (t *relayTrack) Unbind(context webrtc.TrackLocalContext) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	for index := range t.bindings {
		if t.bindings[index].id == context.ID() {
			t.bindings[index] = t.bindings[len(t.bindings)-1]
			t.bindings = t.bindings[:len(t.bindings)-1]
			return nil
		}
	}
	return webrtc.ErrUnbindFailed
}

func (t *relayTrack) ID() string                       { return t.id }
func (*relayTrack) RID() string                        { return "" }
func (t *relayTrack) StreamID() string                 { return t.streamID }
func (t *relayTrack) Kind() webrtc.RTPCodecType        { return codecType(t.codec.MimeType) }
func (t *relayTrack) Codec() webrtc.RTPCodecCapability { return t.codec }

func (t *relayTrack) WriteRTP(packet *rtp.Packet) error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var result error
	for _, binding := range t.bindings {
		header := packet.Header
		header.SSRC = uint32(binding.ssrc)
		header.PayloadType = uint8(binding.payloadType)
		header.Extension = false
		header.ExtensionProfile = 0
		header.Extensions = nil
		if packet.Marker && binding.colorSpaceExtensionID != 0 && len(t.colorData) != 0 {
			if err := header.SetExtension(binding.colorSpaceExtensionID, t.colorData); err != nil {
				result = errors.Join(result, err)
				continue
			}
		}
		if _, err := binding.writeStream.WriteRTP(&header, packet.Payload); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

func codecType(mimeType string) webrtc.RTPCodecType {
	if strings.HasPrefix(strings.ToLower(mimeType), "audio/") {
		return webrtc.RTPCodecTypeAudio
	}
	return webrtc.RTPCodecTypeVideo
}

func colorSpaceExtensionData(rendition Rendition) []byte {
	// H.273: BT.709=1, BT.2020=9, PQ=16, HLG=18. The fourth
	// byte declares limited range with unspecified chroma siting.
	switch rendition.DynamicRange {
	case "sdr":
		return []byte{1, 1, 1, 0x10}
	case "hdr10":
		return []byte{9, 16, 9, 0x10}
	case "hlg":
		return []byte{9, 18, 9, 0x10}
	default:
		return nil
	}
}

func codecMatchesRendition(
	capability webrtc.RTPCodecCapability,
	rendition Rendition,
) bool {
	if codecForMimeType(capability.MimeType) != rendition.Codec {
		return false
	}
	if rendition.Codec != CodecVP9 {
		return true
	}
	expected := "profile-id=" + rendition.Profile
	for _, part := range strings.Split(capability.SDPFmtpLine, ";") {
		if strings.EqualFold(strings.TrimSpace(part), expected) {
			return true
		}
	}
	return false
}

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
	mediaMu      sync.RWMutex
	videoSSRC    map[string]uint32
	writeRTCP    func([]rtcp.Packet) error

	selectedRenditionID   string
	publisherTransceivers map[*webrtc.RTPTransceiver]string
	publisherMIDs         map[string]string

	// Viewer-initiated recovery cooldowns under one mutex. A rate limited
	// request is treated as successfully ignored, never as a failure.
	recoveryMu          sync.Mutex
	lastKeyframeRequest time.Time
	lastICERestart      time.Time

	keyframeCooldown time.Duration
	iceCooldown      time.Duration

	negotiationMu      sync.Mutex
	negotiationPending bool
	iceRestartPending  bool
	pendingCandidates  []ICECandidate
	closeOnce          sync.Once
	recovery           *webrtcrecovery.Controller
	recoveryActive     atomic.Bool
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
		keyframeCooldown: 2 * time.Second,
		iceCooldown:      10 * time.Second,
		videoSSRC:        make(map[string]uint32),
		writeRTCP:        peer.WriteRTCP,
		publisherMIDs:    make(map[string]string),
		recovery: webrtcrecovery.New(
			webrtcrecovery.Policy{
				InitialDelay: 2 * time.Second,
				RetryDelay:   8 * time.Second,
				MaxAttempts:  4,
			},
		),
	}
}

func (s *session) installCallbacks() {
	s.peer.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		safego.Run("stream/onICECandidate", func() {
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
				s.scheduleFail("stream signaling connection was lost")
			}
		})
	})

	s.peer.OnConnectionStateChange(
		func(state webrtc.PeerConnectionState) {
			safego.GuardedRun("stream/onConnectionStateChange", func() {
				s.handleConnectionState(state)
			}, func() {
				s.scheduleFail("internal error in connection state handler")
			})
		},
	)

	if s.role == sessionPublisher {
		s.peer.OnTrack(
			func(
				remote *webrtc.TrackRemote,
				receiver *webrtc.RTPReceiver,
			) {
				safego.GuardedRun("stream/onTrack", func() {
					s.forwardTrack(remote, receiver)
				}, func() {
					s.scheduleFail("internal error while forwarding stream track")
				})
			},
		)
	}
}

// scheduleFail tears the session down behind a panic boundary so a failure
// path can never itself crash the process.
func (s *session) scheduleFail(reason string) {
	safego.Go("stream/failSession", func() {
		s.manager.failSession(s, reason)
	})
}

func (s *session) forwardTrack(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	if s.closed.Load() ||
		(remote.Kind() != webrtc.RTPCodecTypeVideo &&
			remote.Kind() != webrtc.RTPCodecTypeAudio) {

		return
	}
	key := roomTrackKey{kind: remote.Kind()}
	rendition := Rendition{}
	if remote.Kind() == webrtc.RTPCodecTypeVideo {
		transceiver := receiver.RTPTransceiver()
		mid := ""
		if transceiver != nil {
			mid = transceiver.Mid()
		}
		s.mediaMu.RLock()
		key.renditionID = s.publisherMIDs[mid]
		s.mediaMu.RUnlock()
		var exists bool
		rendition, exists = s.room.rendition(key.renditionID)
		if !exists || !codecMatchesRendition(remote.Codec().RTPCodecCapability, rendition) {
			s.manager.failSession(s, "publisher negotiated an unexpected stream rendition")
			return
		}
	}

	trackName := "screen-video"
	if remote.Kind() == webrtc.RTPCodecTypeAudio {
		trackName = "screen-audio"
	}
	local := newRelayTrack(
		remote.Codec().RTPCodecCapability,
		trackName+"-"+s.connectionID,
		"stream-"+strconv.FormatInt(s.channelID, 10),
		rendition,
	)

	if !s.room.addTrack(roomTrack{
		key: key, track: local, codec: remote.Codec().RTPCodecCapability,
		rendition: rendition,
	}) {
		s.manager.failSession(
			s,
			"duplicate stream media track",
		)
		return
	}
	if remote.Kind() == webrtc.RTPCodecTypeVideo {
		s.mediaMu.Lock()
		s.videoSSRC[key.renditionID] = uint32(remote.SSRC())
		s.mediaMu.Unlock()
		// Ask for the first key frame once. Further key frames are requested
		// when a viewer connects or explicitly reports picture loss.
		s.requestKeyFrame(key.renditionID)
	}
	s.room.synchronizeViewers()
	defer func() {
		if remote.Kind() == webrtc.RTPCodecTypeVideo {
			s.mediaMu.Lock()
			delete(s.videoSSRC, key.renditionID)
			s.mediaMu.Unlock()
		}
		if s.room.removeTrack(key, local) {
			if remote.Kind() == webrtc.RTPCodecTypeVideo {
				s.room.disconnectViewersForRendition(
					key.renditionID,
					"selected stream rendition ended",
				)
			} else {
				s.room.synchronizeViewers()
			}
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
			s.scheduleFail(limitReason)
			return
		}

		if err := local.WriteRTP(packet); err != nil {
			return
		}
	}
}

func (s *session) createOffer() error {
	s.negotiationMu.Lock()
	if s.closed.Load() {
		s.negotiationMu.Unlock()
		return ErrSessionNotFound
	}
	sdp, err := s.createOfferLocked(false)
	if err == nil && s.role == sessionPublisher {
		err = s.bindPublisherMIDs()
	}
	s.negotiationMu.Unlock()
	if err != nil {
		return err
	}
	s.sendOffer(sdp)
	return nil
}

func (s *session) bindPublisherMIDs() error {
	mids := make(map[string]string, len(s.publisherTransceivers))
	for transceiver, renditionID := range s.publisherTransceivers {
		mid := transceiver.Mid()
		if mid == "" {
			return errors.New("publisher video transceiver MID is missing")
		}
		if _, duplicate := mids[mid]; duplicate {
			return errors.New("publisher video transceiver MID is duplicated")
		}
		mids[mid] = renditionID
	}
	s.mediaMu.Lock()
	s.publisherMIDs = mids
	s.mediaMu.Unlock()
	return nil
}

func (s *session) synchronizeViewerTracks(force bool) error {
	if s.role != sessionViewer {
		return nil
	}
	tracks := s.room.trackSnapshot(s.selectedRenditionID)
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
		if value.key.kind == webrtc.RTPCodecTypeVideo {
			transceiver := transceiverForSender(s.peer, sender)
			if transceiver == nil {
				s.negotiationMu.Unlock()
				return errors.New("outgoing stream video transceiver is missing")
			}
			if err := transceiver.SetCodecPreferences(
				s.manager.viewerCodecPreferences(value.codec),
			); err != nil {
				s.negotiationMu.Unlock()
				return fmt.Errorf(
					"prefer outgoing %s:%s stream codec: %w",
					value.rendition.Codec, value.rendition.Profile, err,
				)
			}
		}
		safego.Go("stream/drainViewerRTCP", func() {
			drainViewerRTCP(sender, s.room, value.key.renditionID)
		})
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

	sdp, err := s.createOfferLocked(false)
	s.negotiationMu.Unlock()
	if err != nil {
		return err
	}
	s.sendOffer(sdp)
	return nil
}

func transceiverForSender(peer *webrtc.PeerConnection, sender *webrtc.RTPSender) *webrtc.RTPTransceiver {
	for _, transceiver := range peer.GetTransceivers() {
		if transceiver.Sender() == sender {
			return transceiver
		}
	}
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
		return "", fmt.Errorf("create stream WebRTC offer: %w", err)
	}

	s.negotiationPending = false
	return offer.SDP, nil
}

func (s *session) sendOffer(sdp string) {
	if !s.manager.sink.SendOffer(s.connectionID, sdp) {
		s.scheduleFail("stream signaling connection was lost")
	}
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
	restartPending := s.iceRestartPending
	candidates := append(
		[]ICECandidate(nil),
		s.pendingCandidates...,
	)
	if err == nil {
		s.negotiationPending = false
		s.iceRestartPending = false
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
	if restartPending {
		return s.restartICE()
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
	return s.peer.AddICECandidate(
		webrtc.ICECandidateInit{
			Candidate:        candidate.Candidate,
			SDPMid:           candidate.SDPMid,
			SDPMLineIndex:    candidate.SDPMLineIndex,
			UsernameFragment: candidate.UsernameFragment,
		},
	)
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

func (s *session) close() {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		s.recoveryActive.Store(false)
		s.recovery.Stop()
		_ = s.peer.Close()
	})
}

func (s *session) handleConnectionState(
	state webrtc.PeerConnectionState,
) {
	switch state {
	case webrtc.PeerConnectionStateConnected:
		s.recovery.Stop()
		if s.recoveryActive.Swap(false) {
			total := streamRecoverySucceededTotal.Add(1)
			log.Printf(
				"event=stream_media_recovery_succeeded total=%d connection_id=%s role=%s server_id=%d channel_id=%d",
				total, s.connectionID, s.roleName(), s.serverID, s.channelID,
			)
		}
		if s.role == sessionViewer {
			s.room.requestKeyFrame(s.selectedRenditionID)
		}

	case webrtc.PeerConnectionStateDisconnected:
		s.startRecovery(false)

	case webrtc.PeerConnectionStateFailed:
		s.startRecovery(true)

	case webrtc.PeerConnectionStateClosed:
		if !s.closed.Load() {
			s.scheduleFail("stream WebRTC connection closed")
		}

	default:
	}
}

func (s *session) startRecovery(immediate bool) {
	if s.closed.Load() {
		return
	}
	if s.recoveryActive.CompareAndSwap(false, true) {
		total := streamRecoveryStartedTotal.Add(1)
		log.Printf(
			"event=stream_media_recovery_started total=%d connection_id=%s role=%s server_id=%d channel_id=%d immediate=%t",
			total, s.connectionID, s.roleName(), s.serverID, s.channelID, immediate,
		)
	}
	s.recovery.Start(
		immediate,
		s.restartICE,
		func() {
			if s.closed.Load() ||
				s.peer.ConnectionState() == webrtc.PeerConnectionStateConnected {

				return
			}
			s.recoveryActive.Store(false)
			total := streamRecoveryExhaustedTotal.Add(1)
			log.Printf(
				"event=stream_media_recovery_exhausted total=%d connection_id=%s role=%s server_id=%d channel_id=%d",
				total, s.connectionID, s.roleName(), s.serverID, s.channelID,
			)
			s.manager.failSession(
				s,
				"stream WebRTC recovery exhausted",
			)
		},
	)
}

func (s *session) roleName() string {
	if s.role == sessionPublisher {
		return "publisher"
	}
	return "viewer"
}

func (s *session) restartICE() error {
	s.negotiationMu.Lock()
	if s.closed.Load() {
		s.negotiationMu.Unlock()
		return ErrSessionNotFound
	}
	if s.peer.SignalingState() != webrtc.SignalingStateStable {
		s.iceRestartPending = true
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

func (s *session) requestKeyFrame(renditionID string) {
	s.mediaMu.RLock()
	ssrc := s.videoSSRC[renditionID]
	s.mediaMu.RUnlock()
	if s.closed.Load() || ssrc == 0 {
		return
	}
	if s.writeRTCP == nil {
		return
	}
	_ = s.writeRTCP([]rtcp.Packet{
		&rtcp.PictureLossIndication{MediaSSRC: ssrc},
	})
}

// requestRecovery handles a viewer-initiated recovery request with per-action
// cooldowns. Rate limited requests return nil: the viewer should treat them
// as acknowledged but ignored.
func (s *session) requestRecovery(action string) error {
	now := time.Now()
	s.recoveryMu.Lock()
	switch action {
	case RecoveryActionKeyframe:
		if now.Sub(s.lastKeyframeRequest) < s.keyframeCooldown {
			s.recoveryMu.Unlock()
			return nil
		}
		s.lastKeyframeRequest = now
		s.recoveryMu.Unlock()
		s.room.requestKeyFrame(s.selectedRenditionID)
		return nil

	case RecoveryActionICERestart:
		if now.Sub(s.lastICERestart) < s.iceCooldown {
			s.recoveryMu.Unlock()
			return nil
		}
		s.lastICERestart = now
		s.recoveryMu.Unlock()
		return s.restartICE()

	default:
		s.recoveryMu.Unlock()
		return ErrRecoveryActionInvalid
	}
}

func drainViewerRTCP(sender *webrtc.RTPSender, room *room, renditionID string) {
	for {
		packets, _, err := sender.ReadRTCP()
		if err != nil {
			return
		}
		for _, packet := range packets {
			switch packet.(type) {
			case *rtcp.PictureLossIndication,
				*rtcp.FullIntraRequest:

				room.requestKeyFrame(renditionID)
			}
		}
	}
}
