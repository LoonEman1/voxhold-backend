package stream

import (
	"errors"
	"fmt"
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

	mu                        sync.RWMutex
	rooms                     map[int64]*room
	sessions                  map[string]*session
	closed                    bool
	maxViewers                int
	maxVideoBitrateKbps       int
	maxStreamAudioBitrateKbps int
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
	if err := media.RegisterCodec(
		webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType:  webrtc.MimeTypeVP8,
				ClockRate: 90000,
			},
			PayloadType: 96,
		},
		webrtc.RTPCodecTypeVideo,
	); err != nil {
		_ = udpMux.Close()
		return nil, fmt.Errorf("register VP8 codec: %w", err)
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
	hasAudio bool,
) error {
	if connectionID == "" || userID <= 0 ||
		serverID <= 0 || channelID <= 0 {

		return errors.New("invalid stream identifiers")
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
	streamRoom := newRoom(channelID)
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

	if _, err = peer.AddTransceiverFromKind(
		webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly,
		},
	); err != nil {
		_ = peer.Close()
		m.removeRoom(channelID, streamRoom)
		return fmt.Errorf("add incoming video transceiver: %w", err)
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

func (m *Manager) Watch(
	connectionID string,
	userID int64,
	serverID int64,
	channelID int64,
) error {
	if connectionID == "" || userID <= 0 ||
		serverID <= 0 || channelID <= 0 {

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
