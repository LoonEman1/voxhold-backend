package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"voxhold-backend/internal/account"
	"voxhold-backend/internal/antiabuse"
	"voxhold-backend/internal/diagnostics"
	"voxhold-backend/internal/instancebootstrap"
	"voxhold-backend/internal/voice"

	accounthttp "voxhold-backend/internal/account/http"
	accountSqlite "voxhold-backend/internal/account/sqlite"
	serverDomain "voxhold-backend/internal/server"
	serverhttp "voxhold-backend/internal/server/http"
	serverSqlite "voxhold-backend/internal/server/sqlite"
	"voxhold-backend/internal/storage"
	"voxhold-backend/internal/stream"

	channelDomain "voxhold-backend/internal/channel"
	channelhttp "voxhold-backend/internal/channel/http"
	channelSqlite "voxhold-backend/internal/channel/sqlite"

	diagnosticshttp "voxhold-backend/internal/diagnostics/http"
	diagnosticsSqlite "voxhold-backend/internal/diagnostics/sqlite"
	inviteDomain "voxhold-backend/internal/invite"
	invitehttp "voxhold-backend/internal/invite/http"
	inviteSqlite "voxhold-backend/internal/invite/sqlite"
	messageDomain "voxhold-backend/internal/message"
	messagehttp "voxhold-backend/internal/message/http"
	messageSqlite "voxhold-backend/internal/message/sqlite"
	profileDomain "voxhold-backend/internal/profile"
	profilehttp "voxhold-backend/internal/profile/http"
	profileSqlite "voxhold-backend/internal/profile/sqlite"
	readstateDomain "voxhold-backend/internal/readstate"
	readstatehttp "voxhold-backend/internal/readstate/http"
	readstateSqlite "voxhold-backend/internal/readstate/sqlite"

	realtimehttp "voxhold-backend/internal/realtime/http"
	webrtcconfighttp "voxhold-backend/internal/webrtcconfig/http"

	realtimeDomain "voxhold-backend/internal/realtime"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 2 * time.Minute
	shutdownTimeout   = 10 * time.Second
	maxHeaderBytes    = 32 * 1024
)

func main() {
	antiAbuseConfig, err := antiabuse.ConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	antiAbuseGuard := antiabuse.New(antiAbuseConfig)

	db, err := storage.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := instancebootstrap.Validate(context.Background(), db); err != nil {
		log.Fatal(err)
	}

	log.Println("database is ready")

	// Browser clients receive the CLIENT ICE configuration through the
	// runtime endpoint; the backend's own Pion sessions use SERVER values so
	// a public deployment does not create TURN allocations per media
	// session. Legacy WEBRTC_ICE_* act as fallbacks, keeping deployments
	// written before the split working unchanged.
	firstNonEmpty := func(values ...string) string {
		for _, value := range values {
			if value != "" {
				return value
			}
		}
		return ""
	}
	splitICEURLs := func(raw string) []string {
		urls := make([]string, 0, 4)
		for _, part := range strings.Split(raw, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				urls = append(urls, trimmed)
			}
		}
		return urls
	}

	clientICEUsername := firstNonEmpty(
		os.Getenv("WEBRTC_CLIENT_ICE_USERNAME"),
		os.Getenv("WEBRTC_ICE_USERNAME"),
	)
	clientICECredential := firstNonEmpty(
		os.Getenv("WEBRTC_CLIENT_ICE_CREDENTIAL"),
		os.Getenv("WEBRTC_ICE_CREDENTIAL"),
	)
	clientICEURLs := splitICEURLs(firstNonEmpty(
		os.Getenv("WEBRTC_CLIENT_ICE_SERVERS"),
		os.Getenv("WEBRTC_ICE_SERVERS"),
	))

	serverICEUsername := firstNonEmpty(
		os.Getenv("WEBRTC_SERVER_ICE_USERNAME"),
		os.Getenv("WEBRTC_ICE_USERNAME"),
	)
	serverICECredential := firstNonEmpty(
		os.Getenv("WEBRTC_SERVER_ICE_CREDENTIAL"),
		os.Getenv("WEBRTC_ICE_CREDENTIAL"),
	)
	serverICEServers := firstNonEmpty(
		os.Getenv("WEBRTC_SERVER_ICE_SERVERS"),
		os.Getenv("WEBRTC_ICE_SERVERS"),
	)

	realtimeHub := realtimeDomain.NewHub()
	voiceConfig, err := voice.NewConfig(
		os.Getenv("WEBRTC_UDP_PORT"),
		os.Getenv("WEBRTC_MAX_PARTICIPANTS"),
		os.Getenv("WEBRTC_MAX_AUDIO_BITRATE_KBPS"),
		os.Getenv("WEBRTC_PUBLIC_IP"),
		serverICEServers,
		serverICEUsername,
		serverICECredential,
	)
	if err != nil {
		log.Fatal(err)
	}

	voiceManager, err := voice.NewManager(
		voiceConfig,
		realtimeDomain.NewVoiceSignalSink(realtimeHub),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer voiceManager.Close()

	realtimeHub.SetVoiceSessionCloser(voiceManager)
	log.Printf(
		"WebRTC audio is listening on UDP port %d (max %d Kbit/s per microphone)",
		voiceConfig.UDPPort,
		voiceConfig.MaxAudioBitrateKbps,
	)

	streamConfig, err := stream.NewConfig(
		os.Getenv("WEBRTC_STREAM_UDP_PORT"),
		os.Getenv("WEBRTC_STREAM_MAX_VIEWERS"),
		os.Getenv("WEBRTC_STREAM_MAX_P2P_VIEWERS"),
		os.Getenv("WEBRTC_STREAM_MAX_VIDEO_BITRATE_KBPS"),
		os.Getenv("WEBRTC_STREAM_MAX_AUDIO_BITRATE_KBPS"),
		os.Getenv("WEBRTC_PUBLIC_IP"),
		serverICEServers,
		serverICEUsername,
		serverICECredential,
	)
	if err != nil {
		log.Fatal(err)
	}
	if !equalICEConfig(voiceConfig, streamConfig) {
		log.Fatal(
			"WEBRTC_ICE_* environment values must be identical for voice and stream; " +
				"voice and stream ICE configurations diverge",
		)
	}

	streamManager, err := stream.NewManager(
		streamConfig,
		realtimeDomain.NewStreamSignalSink(realtimeHub),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer streamManager.Close()
	realtimeHub.SetStreamSessionCloser(
		streamManager,
		streamConfig.MaxViewers,
		streamConfig.MaxP2PViewers,
	)
	log.Printf(
		"WebRTC streams are listening on UDP port %d (max %d viewers, %d Kbit/s video, %d Kbit/s stream audio)",
		streamConfig.UDPPort,
		streamConfig.MaxViewers,
		streamConfig.MaxVideoBitrateKbps,
		streamConfig.MaxAudioBitrateKbps,
	)

	serverEventPublisher :=
		realtimeDomain.NewServerEventPublisher(realtimeHub)
	channelEventPublisher :=
		realtimeDomain.NewChannelEventPublisher(realtimeHub)
	inviteEventPublisher :=
		realtimeDomain.NewInviteEventPublisher(realtimeHub)

	userRepository := accountSqlite.NewUserRepository(db)
	sessionRepository := accountSqlite.NewSessionRepository(db)
	inviteRegistrationRepository :=
		accountSqlite.NewInviteRegistrationRepository(db)

	accountService := account.NewService(
		userRepository,
		sessionRepository,
		inviteRegistrationRepository,
		realtimeHub,
		realtimeHub,
		serverEventPublisher,
	)
	accountHandler := accounthttp.NewHandler(
		accountService,
		antiAbuseGuard,
	)

	serverRepository := serverSqlite.NewRepository(db)
	serverService := serverDomain.NewService(
		serverRepository,
		realtimeHub,
		serverEventPublisher,
	)
	serverHandler := serverhttp.NewHandler(serverService)

	channelRepository := channelSqlite.NewRepository(db)
	channelService := channelDomain.NewService(
		channelRepository,
		channelEventPublisher,
	)
	channelHandler := channelhttp.NewHandler(channelService)

	inviteRepository := inviteSqlite.NewRepository(db)
	inviteService := inviteDomain.NewService(
		inviteRepository,
		realtimeHub,
		inviteEventPublisher,
		serverEventPublisher,
	)
	inviteHandler := invitehttp.NewHandler(inviteService)

	profileRepository := profileSqlite.NewRepository(db)
	profileService := profileDomain.NewService(profileRepository)
	profileHandler := profilehttp.NewHandler(profileService)

	messageEventPublisher :=
		realtimeDomain.NewMessageEventPublisher(
			realtimeHub,
		)

	messageRepository := messageSqlite.NewRepository(db)

	messageService := messageDomain.NewService(
		messageRepository,
		messageEventPublisher,
	)

	messageHandler := messagehttp.NewHandler(
		messageService,
	)

	readEventPublisher :=
		realtimeDomain.NewReadEventPublisher(realtimeHub)
	readRepository := readstateSqlite.NewRepository(db)
	readService := readstateDomain.NewService(
		readRepository,
		readEventPublisher,
	)
	readHandler := readstatehttp.NewHandler(readService)

	diagnosticsRepository := diagnosticsSqlite.NewRepository(db)
	diagnosticsService := diagnostics.NewService(diagnosticsRepository)
	diagnosticsHandler := diagnosticshttp.NewHandler(diagnosticsService)

	webSocketHandler := realtimehttp.NewHandler(
		accountService,
		channelService,
		serverService,
		readService,
		voiceManager,
		streamManager,
		realtimeHub,
		antiAbuseGuard,
	)

	webrtcConfigHandler := webrtcconfighttp.NewHandler(
		clientICEURLs,
		clientICEUsername,
		clientICECredential,
	)

	mux := http.NewServeMux()
	accountHandler.RegisterRoutes(mux)
	webrtcConfigHandler.RegisterRoutes(
		mux,
		accountHandler.RequireAuth,
	)
	serverHandler.RegisterRoutes(
		mux,
		accountHandler.RequireAuth,
	)
	channelHandler.RegisterRoutes(
		mux,
		accountHandler.RequireAuth,
	)

	inviteHandler.RegisterRoutes(
		mux,
		accountHandler.RequireAuth,
	)

	profileHandler.RegisterRoutes(
		mux,
		accountHandler.RequireAuth,
	)

	messageHandler.RegisterRoutes(
		mux,
		accountHandler.RequireAuth,
	)

	readHandler.RegisterRoutes(
		mux,
		accountHandler.RequireAuth,
	)
	diagnosticsHandler.RegisterRoutes(
		mux,
		accountHandler.RequireAuth,
	)

	webSocketHandler.RegisterRoutes(mux)

	// Liveness: the process is up. Deliberately dependency-free.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// Readiness: dependencies (the SQLite database) are actually usable.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 500*time.Millisecond)
		defer cancel()

		if err := db.PingContext(ctx); err != nil {
			log.Printf("readiness probe failed: %v", err)
			http.Error(w, "database not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}
	listenAddress := os.Getenv("HTTP_LISTEN_ADDRESS")
	if listenAddress == "" {
		listenAddress = "0.0.0.0"
	}

	server := &http.Server{
		Addr:              net.JoinHostPort(listenAddress, port),
		Handler:           antiAbuseGuard.ProtectHTTP(mux),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}

	log.Printf("server started on: %s", server.Addr)

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	signalContext, stopSignals := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stopSignals()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
		return

	case <-signalContext.Done():
		log.Println("shutdown signal received")
	}

	shutdownContext, cancelShutdown := context.WithTimeout(
		context.Background(),
		shutdownTimeout,
	)
	defer cancelShutdown()

	if err := server.Shutdown(shutdownContext); err != nil {
		log.Printf("graceful HTTP shutdown: %v", err)

		if closeErr := server.Close(); closeErr != nil {
			log.Printf("force HTTP shutdown: %v", closeErr)
		}
	}

	realtimeHub.Close()

	log.Println("server stopped")
}

func equalICEConfig(voice voice.Config, stream stream.Config) bool {
	if len(voice.ICEServerURLs) != len(stream.ICEServerURLs) {
		return false
	}
	for index, url := range voice.ICEServerURLs {
		if stream.ICEServerURLs[index] != url {
			return false
		}
	}
	return voice.ICEUsername == stream.ICEUsername &&
		voice.ICECredential == stream.ICECredential
}
