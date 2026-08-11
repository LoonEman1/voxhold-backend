package voice

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
)

var (
	ErrConnectionIDRequired = errors.New(
		"voice connection ID is required",
	)
	ErrSessionNotFound = errors.New(
		"WebRTC voice session not found",
	)
	ErrRoomFull = errors.New("voice channel is full")
)

type Manager struct {
	api           *webrtc.API
	configuration webrtc.Configuration
	sink          SignalSink
	udpMux        ice.UDPMux

	mu              sync.RWMutex
	rooms           map[int64]*room
	sessions        map[string]*session
	closed          bool
	maxParticipants int
}

func NewManager(
	config Config,
	sink SignalSink,
) (*Manager, error) {
	if sink == nil {
		return nil, errors.New("voice signal sink is required")
	}

	if config.MaxParticipants == 0 {
		config.MaxParticipants = DefaultMaxParticipants
	}
	if config.MaxParticipants < 2 || config.MaxParticipants > 100 {
		return nil, ErrMaxParticipantsInvalid
	}

	udpMuxOptions := []ice.UDPMuxFromPortOption{
		ice.UDPMuxFromPortWithNetworks(
			ice.NetworkTypeUDP4,
		),
		ice.UDPMuxFromPortWithReadBufferSize(2 * 1024 * 1024),
		ice.UDPMuxFromPortWithWriteBufferSize(2 * 1024 * 1024),
	}

	// Address rewriting maps every matching local candidate to the same
	// externally reachable address. Including loopback in that case can make
	// Pion keep the rewritten loopback candidate and its socket while dropping
	// the equivalent candidate backed by the real network interface. Such a
	// socket cannot send ICE checks to remote peers. Loopback is only useful
	// when no external address is being advertised.
	if config.PublicIP == "" {
		udpMuxOptions = append(
			udpMuxOptions,
			ice.UDPMuxFromPortWithLoopback(),
		)
	}

	udpMux, err := ice.NewMultiUDPMuxFromPort(
		config.UDPPort,
		udpMuxOptions...,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"listen for WebRTC UDP traffic on port %d: %w",
			config.UDPPort,
			err,
		)
	}

	settingEngine := webrtc.SettingEngine{}
	settingEngine.SetICEUDPMux(udpMux)
	settingEngine.SetICETimeouts(
		10*time.Second,
		30*time.Second,
		2*time.Second,
	)

	if config.PublicIP != "" {
		err = settingEngine.SetICEAddressRewriteRules(
			webrtc.ICEAddressRewriteRule{
				External: []string{config.PublicIP},
				AsCandidateType: webrtc.
					ICECandidateTypeHost,
				Mode: webrtc.ICEAddressRewriteReplace,
				Networks: []webrtc.NetworkType{
					webrtc.NetworkTypeUDP4,
				},
			},
		)
		if err != nil {
			_ = udpMux.Close()
			return nil, fmt.Errorf(
				"configure WebRTC public IP: %w",
				err,
			)
		}
	}

	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterCodec(
		webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType:    webrtc.MimeTypeOpus,
				ClockRate:   48000,
				Channels:    2,
				SDPFmtpLine: "minptime=10;useinbandfec=1",
			},
			PayloadType: 111,
		},
		webrtc.RTPCodecTypeAudio,
	); err != nil {
		_ = udpMux.Close()
		return nil, fmt.Errorf(
			"register Opus codec: %w",
			err,
		)
	}

	interceptorRegistry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(
		mediaEngine,
		interceptorRegistry,
	); err != nil {
		_ = udpMux.Close()
		return nil, fmt.Errorf(
			"register WebRTC interceptors: %w",
			err,
		)
	}

	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(interceptorRegistry),
		webrtc.WithSettingEngine(settingEngine),
	)

	return &Manager{
		api:             api,
		configuration:   newWebRTCConfiguration(config),
		sink:            sink,
		udpMux:          udpMux,
		rooms:           make(map[int64]*room),
		sessions:        make(map[string]*session),
		maxParticipants: config.MaxParticipants,
	}, nil
}

func newWebRTCConfiguration(config Config) webrtc.Configuration {
	if len(config.ICEServerURLs) == 0 {
		return webrtc.Configuration{}
	}

	server := webrtc.ICEServer{
		URLs: config.ICEServerURLs,
	}

	if config.ICEUsername != "" {
		server.Username = config.ICEUsername
		server.Credential = config.ICECredential
		server.CredentialType = webrtc.ICECredentialTypePassword
	}

	return webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{server},
	}
}

func (m *Manager) Join(
	connectionID string,
	userID int64,
	serverID int64,
	channelID int64,
	selfMute bool,
	selfDeaf bool,
) error {
	if connectionID == "" {
		return ErrConnectionIDRequired
	}
	if userID <= 0 || serverID <= 0 || channelID <= 0 {
		return errors.New("voice session identifiers must be positive")
	}

	m.Leave(connectionID)

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("voice manager is closed")
	}

	voiceRoom := m.rooms[channelID]
	if voiceRoom == nil {
		voiceRoom = newRoom(channelID)
		m.rooms[channelID] = voiceRoom
	}
	m.mu.Unlock()

	if !voiceRoom.reserve(m.maxParticipants) {
		return ErrRoomFull
	}

	reservationActive := true
	defer func() {
		if reservationActive {
			voiceRoom.cancelReservation()
			m.removeRoomIfEmpty(channelID, voiceRoom)
		}
	}()

	peerConnection, err := m.api.NewPeerConnection(
		m.configuration,
	)
	if err != nil {
		return fmt.Errorf("create WebRTC peer connection: %w", err)
	}

	if _, err := peerConnection.AddTransceiverFromKind(
		webrtc.RTPCodecTypeAudio,
		webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly,
		},
	); err != nil {
		_ = peerConnection.Close()
		return fmt.Errorf(
			"add incoming audio transceiver: %w",
			err,
		)
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = peerConnection.Close()
		return errors.New("voice manager is closed")
	}

	value := newSession(
		m,
		voiceRoom,
		peerConnection,
		connectionID,
		userID,
		serverID,
		channelID,
		selfMute,
		selfDeaf,
	)

	m.sessions[connectionID] = value
	voiceRoom.addSession(value)
	reservationActive = false
	m.mu.Unlock()

	value.installCallbacks()

	if err := value.synchronizeTracks(true); err != nil {
		m.Leave(connectionID)
		return fmt.Errorf(
			"create initial WebRTC offer: %w",
			err,
		)
	}

	return nil
}

func (m *Manager) removeRoomIfEmpty(
	channelID int64,
	voiceRoom *room,
) {
	if !voiceRoom.empty() {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if voiceRoom.empty() && m.rooms[channelID] == voiceRoom {
		delete(m.rooms, channelID)
	}
}

func (m *Manager) SetState(
	connectionID string,
	selfMute bool,
	selfDeaf bool,
) error {
	value := m.session(connectionID)
	if value == nil {
		return ErrSessionNotFound
	}

	deafChanged := value.setState(selfMute, selfDeaf)
	if deafChanged {
		return value.synchronizeTracks(false)
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

	if err := value.acceptAnswer(sdp); err != nil {
		return fmt.Errorf("accept WebRTC answer: %w", err)
	}

	return nil
}

func (m *Manager) AddICECandidate(
	connectionID string,
	candidate ICECandidate,
) error {
	value := m.session(connectionID)
	if value == nil {
		return ErrSessionNotFound
	}

	if err := value.addICECandidate(candidate); err != nil {
		return fmt.Errorf("add WebRTC ICE candidate: %w", err)
	}

	return nil
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

	delete(m.sessions, connectionID)
	voiceRoom := value.room
	value.closed.Store(true)
	trackRemoved := voiceRoom.removeSession(connectionID)
	roomEmpty := voiceRoom.empty()
	if roomEmpty && m.rooms[value.channelID] == voiceRoom {
		delete(m.rooms, value.channelID)
	}
	m.mu.Unlock()

	value.close()

	if trackRemoved && !roomEmpty {
		voiceRoom.synchronizeSessions()
	}
}

func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}

	m.closed = true
	sessions := make([]*session, 0, len(m.sessions))
	for _, value := range m.sessions {
		sessions = append(sessions, value)
	}

	clear(m.sessions)
	clear(m.rooms)
	m.mu.Unlock()

	for _, value := range sessions {
		value.close()
	}

	return m.udpMux.Close()
}

func (m *Manager) session(connectionID string) *session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.sessions[connectionID]
}

func (m *Manager) failSession(
	value *session,
	reason string,
) {
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
	m.sink.CloseVoice(value.connectionID, reason)
}
