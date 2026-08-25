package stream

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
)

type Manager struct {
	api           *webrtc.API
	configuration webrtc.Configuration
	sink          SignalSink
	udpMux        ice.UDPMux

	videoCodecDescriptors []videoCodecDescriptor

	mu                        sync.RWMutex
	rooms                     map[int64]*room
	sessions                  map[string]*session
	closed                    bool
	maxViewers                int
	maxVideoBitrateKbps       int
	maxStreamAudioBitrateKbps int
}

// Rendition is the validated media contract passed from realtime signaling to
// the SFU. The SFU does not transcode it; it only negotiates and routes it.
type Rendition struct {
	ID             string
	Codec          Codec
	Profile        string
	DynamicRange   string
	BitDepth       int
	ColorPrimaries string
	Transfer       string
	Matrix         string
}

func NewManager(config Config, sink SignalSink) (*Manager, error) {
	if sink == nil {
		return nil, errors.New("stream signal sink is required")
	}
	if config.MaxViewers == 0 {
		config.MaxViewers = DefaultMaxViewers
	}
	if config.MaxVideoBitrateKbps == 0 {
		config.MaxVideoBitrateKbps = DefaultMaxVideoBitrateKbps
	}
	if config.MaxAudioBitrateKbps == 0 {
		config.MaxAudioBitrateKbps = DefaultMaxAudioBitrateKbps
	}
	if config.MaxViewers < 1 || config.MaxViewers > 100 {
		return nil, ErrMaxViewersInvalid
	}
	if config.MaxVideoBitrateKbps < 500 ||
		config.MaxVideoBitrateKbps > 20000 {

		return nil, ErrMaxVideoBitrateInvalid
	}
	if config.MaxAudioBitrateKbps < 32 ||
		config.MaxAudioBitrateKbps > 510 {

		return nil, ErrMaxAudioBitrateInvalid
	}

	options := []ice.UDPMuxFromPortOption{
		ice.UDPMuxFromPortWithNetworks(ice.NetworkTypeUDP4),
		ice.UDPMuxFromPortWithReadBufferSize(4 * 1024 * 1024),
		ice.UDPMuxFromPortWithWriteBufferSize(4 * 1024 * 1024),
	}
	if config.PublicIP == "" {
		options = append(options, ice.UDPMuxFromPortWithLoopback())
	}

	udpMux, err := ice.NewMultiUDPMuxFromPort(config.UDPPort, options...)
	if err != nil {
		return nil, fmt.Errorf(
			"listen for stream WebRTC UDP traffic on port %d: %w",
			config.UDPPort,
			err,
		)
	}

	settings := webrtc.SettingEngine{}
	settings.SetICEUDPMux(udpMux)
	settings.SetICETimeouts(
		10*time.Second,
		30*time.Second,
		2*time.Second,
	)
	if config.PublicIP != "" {
		err = settings.SetICEAddressRewriteRules(
			webrtc.ICEAddressRewriteRule{
				External:        []string{config.PublicIP},
				AsCandidateType: webrtc.ICECandidateTypeHost,
				Mode:            webrtc.ICEAddressRewriteReplace,
				Networks: []webrtc.NetworkType{
					webrtc.NetworkTypeUDP4,
				},
			},
		)
		if err != nil {
			_ = udpMux.Close()
			return nil, fmt.Errorf(
				"configure stream WebRTC public IP: %w",
				err,
			)
		}
	}

	media := &webrtc.MediaEngine{}
	descriptors, err := registerStreamCodecs(media)
	if err != nil {
		_ = udpMux.Close()
		return nil, err
	}
	if err := media.RegisterCodec(
		webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType:  webrtc.MimeTypeOpus,
				ClockRate: 48000,
				Channels:  2,
				SDPFmtpLine: fmt.Sprintf(
					"minptime=10;useinbandfec=1;stereo=1;sprop-stereo=1;maxaveragebitrate=%d",
					config.MaxAudioBitrateKbps*1000,
				),
			},
			PayloadType: 111,
		},
		webrtc.RTPCodecTypeAudio,
	); err != nil {
		_ = udpMux.Close()
		return nil, fmt.Errorf("register stream Opus codec: %w", err)
	}
	if err := media.RegisterHeaderExtension(
		webrtc.RTPHeaderExtensionCapability{URI: colorSpaceRTPHeaderExtensionURI},
		webrtc.RTPCodecTypeVideo,
	); err != nil {
		_ = udpMux.Close()
		return nil, fmt.Errorf("register stream color-space RTP extension: %w", err)
	}

	registry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(
		media,
		registry,
	); err != nil {
		_ = udpMux.Close()
		return nil, fmt.Errorf(
			"register stream WebRTC interceptors: %w",
			err,
		)
	}

	return &Manager{
		api: webrtc.NewAPI(
			webrtc.WithMediaEngine(media),
			webrtc.WithInterceptorRegistry(registry),
			webrtc.WithSettingEngine(settings),
		),
		configuration:             newWebRTCConfiguration(config),
		sink:                      sink,
		udpMux:                    udpMux,
		videoCodecDescriptors:     descriptors,
		rooms:                     make(map[int64]*room),
		sessions:                  make(map[string]*session),
		maxViewers:                config.MaxViewers,
		maxVideoBitrateKbps:       config.MaxVideoBitrateKbps,
		maxStreamAudioBitrateKbps: config.MaxAudioBitrateKbps,
	}, nil
}

func newWebRTCConfiguration(config Config) webrtc.Configuration {
	if len(config.ICEServerURLs) == 0 {
		return webrtc.Configuration{}
	}
	server := webrtc.ICEServer{URLs: config.ICEServerURLs}
	if config.ICEUsername != "" {
		server.Username = config.ICEUsername
		server.Credential = config.ICECredential
		server.CredentialType = webrtc.ICECredentialTypePassword
	}
	return webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{server},
	}
}

func (m *Manager) Start(
	connectionID string,
	userID int64,
	serverID int64,
	channelID int64,
	codec Codec,
	hasAudio bool,
) error {
	return m.StartWithRenditions(
		connectionID, userID, serverID, channelID, codec, hasAudio,
		[]Rendition{legacySDRRendition(codec)},
	)
}

func (m *Manager) StartWithRenditions(
	connectionID string,
	userID int64,
	serverID int64,
	channelID int64,
	codec Codec,
	hasAudio bool,
	renditions []Rendition,
) error {
	if connectionID == "" || userID <= 0 ||
		serverID <= 0 || channelID <= 0 || !validCodec(codec) ||
		!validMediaRenditions(renditions) || sdrCodec(renditions) != codec {

		return errors.New("invalid stream identifiers")
	}
	for _, rendition := range renditions {
		if len(codecParametersForRendition(m, rendition)) == 0 {
			return fmt.Errorf("unsupported stream rendition %q", rendition.ID)
		}
	}

	m.Leave(connectionID)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("stream manager is closed")
	}
	if m.rooms[channelID] != nil {
		m.mu.Unlock()
		return ErrStreamExists
	}
	streamRoom := newRoomWithRenditions(channelID, renditions)
	m.rooms[channelID] = streamRoom
	m.mu.Unlock()

	peer, err := m.api.NewPeerConnection(m.configuration)
	if err != nil {
		m.removeRoom(channelID, streamRoom)
		return fmt.Errorf(
			"create publisher peer connection: %w",
			err,
		)
	}

	publisherTransceivers := make(map[*webrtc.RTPTransceiver]string, len(renditions))
	for _, rendition := range renditions {
		videoTransceiver, transceiverErr := peer.AddTransceiverFromKind(
			webrtc.RTPCodecTypeVideo,
			webrtc.RTPTransceiverInit{
				Direction: webrtc.RTPTransceiverDirectionRecvonly,
			},
		)
		if transceiverErr != nil {
			_ = peer.Close()
			m.removeRoom(channelID, streamRoom)
			return fmt.Errorf("add incoming video transceiver: %w", transceiverErr)
		}
		if transceiverErr = videoTransceiver.SetCodecPreferences(
			codecParametersForRendition(m, rendition),
		); transceiverErr != nil {
			_ = peer.Close()
			m.removeRoom(channelID, streamRoom)
			return fmt.Errorf(
				"prefer incoming %s:%s stream codec: %w",
				rendition.Codec, rendition.Profile, transceiverErr,
			)
		}
		publisherTransceivers[videoTransceiver] = rendition.ID
	}
	if hasAudio {
		if _, err = peer.AddTransceiverFromKind(
			webrtc.RTPCodecTypeAudio,
			webrtc.RTPTransceiverInit{
				Direction: webrtc.RTPTransceiverDirectionRecvonly,
			},
		); err != nil {
			_ = peer.Close()
			m.removeRoom(channelID, streamRoom)
			return fmt.Errorf(
				"add incoming stream audio transceiver: %w",
				err,
			)
		}
	}

	value := newSession(
		m,
		streamRoom,
		peer,
		connectionID,
		userID,
		serverID,
		channelID,
		sessionPublisher,
	)
	value.publisherTransceivers = publisherTransceivers
	if !streamRoom.setPublisher(value) {
		_ = peer.Close()
		m.removeRoom(channelID, streamRoom)
		return ErrStreamExists
	}

	m.mu.Lock()
	if m.closed || m.rooms[channelID] != streamRoom {
		m.mu.Unlock()
		_ = peer.Close()
		m.removeRoom(channelID, streamRoom)
		return errors.New("stream manager is closed")
	}
	m.sessions[connectionID] = value
	m.mu.Unlock()

	value.installCallbacks()
	if err := value.createOffer(); err != nil {
		m.Leave(connectionID)
		return err
	}
	return nil
}

func sdrCodec(renditions []Rendition) Codec {
	for _, rendition := range renditions {
		if rendition.DynamicRange == "sdr" {
			return rendition.Codec
		}
	}
	return ""
}

func validMediaRenditions(renditions []Rendition) bool {
	if len(renditions) == 0 || len(renditions) > 2 {
		return false
	}
	ids := make(map[string]struct{}, len(renditions))
	ranges := make(map[string]struct{}, len(renditions))
	hasSDR := false
	for _, value := range renditions {
		if value.ID == "" || len(value.ID) > 32 || value.BitDepth == 0 {
			return false
		}
		if _, duplicate := ids[value.ID]; duplicate {
			return false
		}
		if _, duplicate := ranges[value.DynamicRange]; duplicate {
			return false
		}
		ids[value.ID] = struct{}{}
		ranges[value.DynamicRange] = struct{}{}
		switch value.DynamicRange {
		case "sdr":
			hasSDR = value.BitDepth == 8 && value.ColorPrimaries == "bt709" &&
				value.Transfer == "bt709" && value.Matrix == "bt709" &&
				validSDRProfile(value.Codec, value.Profile)
			if !hasSDR {
				return false
			}
		case "hdr10":
			if value.BitDepth != 10 || value.ColorPrimaries != "bt2020" ||
				value.Transfer != "pq" || value.Matrix != "bt2020-ncl" ||
				!validHDRProfile(value.Codec, value.Profile) {

				return false
			}
		case "hlg":
			if value.BitDepth != 10 || value.ColorPrimaries != "bt2020" ||
				value.Transfer != "hlg" || value.Matrix != "bt2020-ncl" ||
				!validHDRProfile(value.Codec, value.Profile) {

				return false
			}
		default:
			return false
		}
	}
	return hasSDR
}

func validSDRProfile(codec Codec, profile string) bool {
	return (codec == CodecVP8 && profile == "") ||
		(codec == CodecVP9 && profile == "0") ||
		(codec == CodecH264 && profile == "baseline") ||
		(codec == CodecAV1 && profile == "main")
}

func validHDRProfile(codec Codec, profile string) bool {
	return (codec == CodecVP9 && profile == "2") ||
		(codec == CodecAV1 && profile == "main10")
}

func validCodec(codec Codec) bool {
	switch codec {
	case CodecVP8, CodecVP9, CodecH264, CodecAV1:
		return true
	default:
		return false
	}
}

// videoCodecDescriptor describes one registered primary video payload type
// together with its optional RTX retransmission payload type.
type videoCodecDescriptor struct {
	primary webrtc.RTPCodecParameters
	rtxPT   uint8
}

const (
	vp8PrimaryPT   = 96
	vp8RTXPT       = 97
	vp9PrimaryPT   = 98
	vp9RTXPT       = 99
	vp9HDRPrimary  = 100
	vp9HDRRTX      = 101
	h264CBPrimary  = 102
	h264CBRTX      = 103
	h264LegPrimary = 104
	h264LegRTX     = 105
	av1PrimaryPT   = 45
	av1RTXPT       = 46
)

func streamVideoCodecs() []webrtc.RTPCodecParameters {
	result := make([]webrtc.RTPCodecParameters, 0)
	for _, descriptor := range streamVideoCodecDescriptors() {
		result = append(result, descriptor.primary)
	}
	return result
}

func streamVideoCodecDescriptors() []videoCodecDescriptor {
	feedback := []webrtc.RTCPFeedback{
		{Type: "goog-remb"},
		{Type: "ccm", Parameter: "fir"},
		{Type: "nack"},
		{Type: "nack", Parameter: "pli"},
	}
	return []videoCodecDescriptor{
		{
			primary: webrtc.RTPCodecParameters{
				RTPCodecCapability: webrtc.RTPCodecCapability{
					MimeType:     webrtc.MimeTypeVP8,
					ClockRate:    90000,
					RTCPFeedback: feedback,
				},
				PayloadType: vp8PrimaryPT,
			},
			rtxPT: vp8RTXPT,
		},
		{
			primary: webrtc.RTPCodecParameters{
				RTPCodecCapability: webrtc.RTPCodecCapability{
					MimeType:     webrtc.MimeTypeVP9,
					ClockRate:    90000,
					SDPFmtpLine:  "profile-id=0",
					RTCPFeedback: feedback,
				},
				PayloadType: vp9PrimaryPT,
			},
			rtxPT: vp9RTXPT,
		},
		{
			primary: webrtc.RTPCodecParameters{
				RTPCodecCapability: webrtc.RTPCodecCapability{
					MimeType:     webrtc.MimeTypeVP9,
					ClockRate:    90000,
					SDPFmtpLine:  "profile-id=2",
					RTCPFeedback: feedback,
				},
				PayloadType: vp9HDRPrimary,
			},
			rtxPT: vp9HDRRTX,
		},
		{
			// Constrained Baseline is the interoperable WebRTC profile per
			// RFC 7742; it must be offered before any legacy value.
			primary: webrtc.RTPCodecParameters{
				RTPCodecCapability: webrtc.RTPCodecCapability{
					MimeType:     webrtc.MimeTypeH264,
					ClockRate:    90000,
					SDPFmtpLine:  "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
					RTCPFeedback: feedback,
				},
				PayloadType: h264CBPrimary,
			},
			rtxPT: h264CBRTX,
		},
		{
			// Legacy profile kept on a separate payload type for older web
			// clients that match exactly on 42001f.
			primary: webrtc.RTPCodecParameters{
				RTPCodecCapability: webrtc.RTPCodecCapability{
					MimeType:     webrtc.MimeTypeH264,
					ClockRate:    90000,
					SDPFmtpLine:  "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f",
					RTCPFeedback: feedback,
				},
				PayloadType: h264LegPrimary,
			},
			rtxPT: h264LegRTX,
		},
		{
			primary: webrtc.RTPCodecParameters{
				RTPCodecCapability: webrtc.RTPCodecCapability{
					MimeType:     webrtc.MimeTypeAV1,
					ClockRate:    90000,
					RTCPFeedback: feedback,
				},
				PayloadType: av1PrimaryPT,
			},
			rtxPT: av1RTXPT,
		},
	}
}

// registerStreamCodecs registers every primary video codec and its paired
// video/rtx payload type into the media engine.
func registerStreamCodecs(
	media *webrtc.MediaEngine,
) ([]videoCodecDescriptor, error) {
	rtxFeedback := []webrtc.RTCPFeedback{{Type: "goog-remb"}}
	descriptors := make([]videoCodecDescriptor, 0, 5)
	for _, descriptor := range streamVideoCodecDescriptors() {
		if err := media.RegisterCodec(
			descriptor.primary,
			webrtc.RTPCodecTypeVideo,
		); err != nil {
			return nil, fmt.Errorf(
				"register stream video codec %s: %w",
				descriptor.primary.MimeType,
				err,
			)
		}
		rtx := webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType:     "video/rtx",
				ClockRate:    90000,
				SDPFmtpLine:  fmt.Sprintf("apt=%d", descriptor.primary.PayloadType),
				RTCPFeedback: rtxFeedback,
			},
			PayloadType: webrtc.PayloadType(descriptor.rtxPT),
		}
		if err := media.RegisterCodec(rtx, webrtc.RTPCodecTypeVideo); err != nil {
			return nil, fmt.Errorf(
				"register stream video rtx for %s: %w",
				descriptor.primary.MimeType,
				err,
			)
		}
		descriptors = append(descriptors, descriptor)
	}
	return descriptors, nil
}

func legacySDRRendition(codec Codec) Rendition {
	profile := ""
	switch codec {
	case CodecVP9:
		profile = "0"
	case CodecH264:
		profile = "baseline"
	case CodecAV1:
		profile = "main"
	}
	return Rendition{
		ID: "sdr", Codec: codec, Profile: profile, DynamicRange: "sdr",
		BitDepth: 8, ColorPrimaries: "bt709", Transfer: "bt709", Matrix: "bt709",
	}
}

func codecParametersForRendition(
	m *Manager,
	rendition Rendition,
) []webrtc.RTPCodecParameters {
	if rendition.ID == "" || !validCodec(rendition.Codec) {
		return nil
	}
	validProfile := rendition.Profile == ""
	switch rendition.Codec {
	case CodecVP8:
		validProfile = rendition.Profile == ""
	case CodecVP9:
		validProfile = rendition.Profile == "0" || rendition.Profile == "2"
	case CodecH264:
		validProfile = rendition.Profile == "baseline"
	case CodecAV1:
		validProfile = rendition.Profile == "main" || rendition.Profile == "main10"
	}
	if !validProfile {
		return nil
	}
	result := make([]webrtc.RTPCodecParameters, 0, 4)
	for _, descriptor := range m.videoCodecDescriptors {
		primary := descriptor.primary
		if codecForMimeType(primary.MimeType) != rendition.Codec {
			continue
		}
		if rendition.Codec == CodecVP9 &&
			!strings.EqualFold(primary.SDPFmtpLine, "profile-id="+rendition.Profile) {

			continue
		}
		result = append(result, primary)
		if rtx, ok := rtxForDescriptor(descriptor); ok {
			result = append(result, rtx)
		}
	}
	return result
}

// viewerCodecPreferences selects the exact registered payload types that match
// the capability the publisher actually negotiated, plus its paired RTX
// entry. It never substitutes one H.264 profile for another after ingest.
func (m *Manager) viewerCodecPreferences(
	capability webrtc.RTPCodecCapability,
) []webrtc.RTPCodecParameters {
	for _, descriptor := range m.videoCodecDescriptors {
		primary := descriptor.primary.RTPCodecCapability
		if !strings.EqualFold(primary.MimeType, capability.MimeType) ||
			primary.ClockRate != capability.ClockRate ||
			!strings.EqualFold(primary.SDPFmtpLine, capability.SDPFmtpLine) {

			continue
		}
		result := []webrtc.RTPCodecParameters{descriptor.primary}
		if rtx, ok := rtxForDescriptor(descriptor); ok {
			result = append(result, rtx)
		}
		return result
	}
	return []webrtc.RTPCodecParameters{{RTPCodecCapability: capability}}
}

func rtxForDescriptor(
	descriptor videoCodecDescriptor,
) (webrtc.RTPCodecParameters, bool) {
	if descriptor.rtxPT == 0 {
		return webrtc.RTPCodecParameters{}, false
	}
	return webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    "video/rtx",
			ClockRate:   90000,
			SDPFmtpLine: fmt.Sprintf("apt=%d", descriptor.primary.PayloadType),
		},
		PayloadType: webrtc.PayloadType(descriptor.rtxPT),
	}, true
}

func codecForMimeType(mimeType string) Codec {
	switch strings.ToLower(mimeType) {
	case strings.ToLower(webrtc.MimeTypeVP8):
		return CodecVP8
	case strings.ToLower(webrtc.MimeTypeVP9):
		return CodecVP9
	case strings.ToLower(webrtc.MimeTypeH264):
		return CodecH264
	case strings.ToLower(webrtc.MimeTypeAV1):
		return CodecAV1
	default:
		return ""
	}
}

func (m *Manager) Watch(
	connectionID string,
	userID int64,
	serverID int64,
	channelID int64,
) error {
	m.mu.RLock()
	streamRoom := m.rooms[channelID]
	m.mu.RUnlock()
	selectedRenditionID := "sdr"
	if streamRoom != nil {
		selectedRenditionID = streamRoom.defaultSDRRenditionID()
	}
	return m.WatchRendition(
		connectionID, userID, serverID, channelID, selectedRenditionID,
	)
}

func (m *Manager) WatchRendition(
	connectionID string,
	userID int64,
	serverID int64,
	channelID int64,
	selectedRenditionID string,
) error {
	if connectionID == "" || userID <= 0 ||
		serverID <= 0 || channelID <= 0 || selectedRenditionID == "" {

		return errors.New("invalid stream identifiers")
	}

	m.Leave(connectionID)
	m.mu.RLock()
	streamRoom := m.rooms[channelID]
	closed := m.closed
	m.mu.RUnlock()
	if closed {
		return errors.New("stream manager is closed")
	}
	if streamRoom == nil {
		return ErrStreamNotFound
	}
	if !streamRoom.hasRendition(selectedRenditionID) {
		return ErrStreamNotFound
	}
	if !streamRoom.reserveViewer(m.maxViewers) {
		return ErrViewerLimit
	}
	reservationActive := true
	defer func() {
		if reservationActive {
			streamRoom.cancelViewerReservation()
		}
	}()

	peer, err := m.api.NewPeerConnection(m.configuration)
	if err != nil {
		return fmt.Errorf("create viewer peer connection: %w", err)
	}
	value := newSession(
		m,
		streamRoom,
		peer,
		connectionID,
		userID,
		serverID,
		channelID,
		sessionViewer,
	)
	value.selectedRenditionID = selectedRenditionID

	m.mu.Lock()
	if m.closed || m.rooms[channelID] != streamRoom {
		m.mu.Unlock()
		_ = peer.Close()
		return ErrStreamNotFound
	}
	m.sessions[connectionID] = value
	streamRoom.addViewer(value)
	reservationActive = false
	m.mu.Unlock()

	value.installCallbacks()
	if err := value.synchronizeViewerTracks(true); err != nil {
		m.Leave(connectionID)
		return err
	}
	return nil
}

func (m *Manager) AcceptAnswer(
	connectionID string,
	sdp string,
) error {
	value := m.session(connectionID)
	if value == nil {
		return ErrSessionNotFound
	}
	return value.acceptAnswer(sdp)
}

func (m *Manager) AddICECandidate(
	connectionID string,
	candidate ICECandidate,
) error {
	value := m.session(connectionID)
	if value == nil {
		return ErrSessionNotFound
	}
	return value.addICECandidate(candidate)
}

// Recovery actions accepted from a server-mode viewer.
const (
	RecoveryActionKeyframe   = "keyframe"
	RecoveryActionICERestart = "ice_restart"
)

var ErrRecoveryActionInvalid = errors.New("stream recovery action is invalid")

// RequestRecovery applies a bounded viewer-initiated recovery action to the
// session on this connection. The session enforces its own cooldowns; rate
// limited requests are silently ignored and never close the session.
func (m *Manager) RequestRecovery(
	connectionID string,
	action string,
) error {
	switch action {
	case RecoveryActionKeyframe, RecoveryActionICERestart:
	default:
		return ErrRecoveryActionInvalid
	}
	value := m.session(connectionID)
	if value == nil {
		return ErrSessionNotFound
	}
	if value.role != sessionViewer {
		return errors.New("stream recovery requires a viewer session")
	}
	return value.requestRecovery(action)
}

func (m *Manager) Leave(connectionID string) {
	if connectionID == "" {
		return
	}

	m.mu.Lock()
	value := m.sessions[connectionID]
	if value == nil {
		m.mu.Unlock()
		return
	}

	if value.role == sessionPublisher {
		delete(m.rooms, value.channelID)
		values := value.room.closeSnapshot()
		for _, current := range values {
			delete(m.sessions, current.connectionID)
			current.closed.Store(true)
		}
		m.mu.Unlock()

		for _, current := range values {
			current.close()
		}
		return
	}

	delete(m.sessions, connectionID)
	value.room.removeViewer(connectionID)
	value.closed.Store(true)
	m.mu.Unlock()
	value.close()
}

func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	values := make([]*session, 0, len(m.sessions))
	for _, value := range m.sessions {
		values = append(values, value)
	}
	clear(m.sessions)
	clear(m.rooms)
	m.mu.Unlock()

	for _, value := range values {
		value.close()
	}
	return m.udpMux.Close()
}

func (m *Manager) session(connectionID string) *session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[connectionID]
}

func (m *Manager) removeRoom(channelID int64, value *room) {
	m.mu.Lock()
	if m.rooms[channelID] == value {
		delete(m.rooms, channelID)
	}
	m.mu.Unlock()
}

func (m *Manager) failSession(value *session, reason string) {
	if value == nil {
		return
	}
	m.mu.RLock()
	current := m.sessions[value.connectionID]
	m.mu.RUnlock()
	if current != value {
		return
	}

	m.Leave(value.connectionID)
	m.sink.CloseStream(value.connectionID, reason)
}
