package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"sync"
	"syscall"
	"time"
	"voxhold-backend/internal/account"
	"voxhold-backend/internal/antiabuse"
	"voxhold-backend/internal/attachments"
	"voxhold-backend/internal/avatar"
	"voxhold-backend/internal/community"
	"voxhold-backend/internal/cors"
	"voxhold-backend/internal/diagnostics"
	"voxhold-backend/internal/instancebootstrap"
	"voxhold-backend/internal/notifications"
	"voxhold-backend/internal/recordings"
	"voxhold-backend/internal/safego"
	"voxhold-backend/internal/telemetry"
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
	// The Go runtime validates GOAMD64 before main. This preflight therefore
	// verifies the actual image on its target CPU without opening the database.
	if len(os.Args) == 2 && os.Args[1] == "--check-cpu" {
		level := "native"
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range info.Settings {
				if setting.Key == "GOAMD64" {
					level = setting.Value
				}
			}
		}
		fmt.Printf("arch=%s level=%s\n", runtime.GOARCH, level)
		return
	}
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

	corsManager, err := cors.NewManager(context.Background(), db)
	if err != nil {
		log.Fatal(err)
	}

	// Browser clients receive the CLIENT ICE configuration through the
	// runtime endpoint; the backend's own Pion sessions use SERVER values so
	// a public deployment does not create TURN allocations per media
	// session. Legacy WEBRTC_ICE_* act as fallbacks, keeping deployments
	// written before the split working unchanged.
	clientICE, err := resolveICE("WEBRTC_CLIENT_ICE_", os.LookupEnv)
	if err != nil {
		log.Fatal("invalid client ICE configuration: ", err)
	}
	serverICE, err := resolveICE("WEBRTC_SERVER_ICE_", os.LookupEnv)
	if err != nil {
		log.Fatal("invalid server ICE configuration: ", err)
	}
	clientICEURLs := splitICEURLs(clientICE.servers)
	clientICEUsername, clientICECredential := clientICE.username, clientICE.credential
	serverICEServers, serverICEUsername, serverICECredential := serverICE.servers, serverICE.username, serverICE.credential

	realtimeHub := realtimeDomain.NewHub()
	recordingQuota := recordings.DefaultQuotaBytes
	if raw := os.Getenv("RECORDING_STORAGE_QUOTA_BYTES"); raw != "" {
		recordingQuota, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			log.Fatal("invalid recording storage quota")
		}
	}
	recordingRate := 2 << 20
	if raw := os.Getenv("RECORDING_DOWNLOAD_BYTES_PER_SECOND"); raw != "" {
		recordingRate, err = strconv.Atoi(raw)
		if err != nil {
			log.Fatal("invalid recording download rate")
		}
	}
	recordingService, err := recordings.New(db, recordings.Config{Directory: os.Getenv("RECORDING_DIRECTORY"), QuotaBytes: recordingQuota, DownloadBytesPerSecond: recordingRate}, realtimeHub.HasVoiceParticipant, realtimeHub.PublishRecordingChanged)
	if err != nil {
		log.Fatal(err)
	}
	recordingContext, cancelRecordings := context.WithCancel(context.Background())
	var recordingWorker sync.WaitGroup
	recordingWorker.Add(1)
	safego.Go("recordings/cleanup", func() { defer recordingWorker.Done(); recordingService.Run(recordingContext) })
	defer func() { cancelRecordings(); recordingWorker.Wait(); _ = recordingService.Close() }()
	recordingHandler := recordings.NewHandler(recordingService)
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
	diagnosticServer, diagnosticListener, err := telemetry.Listen(os.Getenv("DIAGNOSTICS_LISTEN_ADDR"), func() any {
		return map[string]any{"metrics": telemetry.Snapshot(), "db": db.Stats(), "voice": voiceManager.Stats(), "stream": streamManager.Stats(), "realtime": realtimeHub.Stats(), "goroutines": runtime.NumGoroutine()}
	})
	if err != nil {
		log.Fatal(err)
	}
	if diagnosticServer != nil {
		safego.Go("diagnostics/http", func() {
			if err := diagnosticServer.Serve(diagnosticListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("diagnostics server: %v", err)
			}
		})
		defer diagnosticServer.Close()
	}

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
	realtimeHub.SetChannelAccessLoader(channelRepository.AccessibleChannelIDs)
	realtimeHub.SetChannelAccessBatchLoader(channelRepository.AccessibleChannelIDsForUsers)
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
	realtimeHub.SetLastSeenSaver(func(userID, at int64) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := profileRepository.RecordLastSeen(ctx, userID, at); err != nil {
			log.Printf("persist last seen: %v", err)
		}
	})
	profileService := profileDomain.NewService(profileRepository)
	profileHandler := profilehttp.NewHandler(profileService)
	avatarHandler := avatar.NewHandler(db, profileService, func(userID int64, version *string) {
		rows, err := db.Query("SELECT server_id FROM server_members WHERE user_id = ?", userID)
		if err != nil {
			log.Printf("avatar event membership lookup: %v", err)
			return
		}
		var serverIDs []int64
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				serverIDs = append(serverIDs, id)
			}
		}
		_ = rows.Close()
		realtimeHub.PublishAvatarChanged(serverIDs, userID, version)
	})

	messageEventPublisher :=
		realtimeDomain.NewMessageEventPublisher(
			realtimeHub,
		)
	realtimeHub.SetRolePermissionLoader(func(serverID, userID int64) (int, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var bits int
		err := db.QueryRowContext(ctx, `SELECT permissions FROM member_permissions WHERE server_id=? AND user_id=?`, serverID, userID).Scan(&bits)
		return bits, err
	})
	communityService := community.New(db, realtimeHub.RefreshRolePermissions, func(userID int64, event string, value any) {
		realtimeHub.PublishToUser(userID, realtimeDomain.OutgoingEvent{Type: realtimeDomain.EventType(event), Data: value})
	})

	notificationService := notifications.NewService(db, realtimeHub.ConnectedUserIDs,
		func(userID int64, event notifications.Event) {
			realtimeHub.PublishToUser(userID, realtimeDomain.OutgoingEvent{Type: realtimeDomain.EventNotificationMessage, Data: event})
		},
		func(userID int64, value notifications.Settings) {
			realtimeHub.PublishToUser(userID, realtimeDomain.OutgoingEvent{Type: realtimeDomain.EventNotificationSettingsUpdated, Data: value})
		},
	)
	messageEventPublisher.SetCreatedListener(func(value messageDomain.Message) {
		if err := communityService.RecordThread(value); err != nil {
			log.Printf("follow thread: %v", err)
		}
		if err := notificationService.Deliver(value); err != nil {
			log.Printf("deliver message notification: %v", err)
		}
	})
	messageRepository := messageSqlite.NewRepository(db)

	messageService := messageDomain.NewService(
		messageRepository,
		messageEventPublisher,
	)

	messageHandler := messagehttp.NewHandler(
		messageService,
	)
	attachmentService, err := attachments.New(db, recordingService, func(cid, conversationID int64) {
		if conversationID > 0 {
			rows, err := db.Query(`SELECT user_id FROM conversation_members WHERE conversation_id=?`, conversationID)
			if err != nil {
				return
			}
			var ids []int64
			for rows.Next() {
				var id int64
				if rows.Scan(&id) == nil {
					ids = append(ids, id)
				}
			}
			rows.Close()
			for _, id := range ids {
				realtimeHub.PublishToUser(id, realtimeDomain.OutgoingEvent{Type: "attachments.changed", Data: map[string]int64{"conversation_id": conversationID}})
			}
			return
		}
		realtimeHub.Publish(cid, realtimeDomain.OutgoingEvent{Type: "attachments.changed", Data: map[string]int64{"channel_id": cid}})
	})
	if err != nil {
		log.Fatal(err)
	}
	defer attachmentService.Close()
	communityService.SetMessages(messageService, attachmentService.Copy)
	communityContext, stopCommunity := context.WithCancel(context.Background())
	var communityWorker sync.WaitGroup
	communityWorker.Add(1)
	safego.Go("community/worker", func() { defer communityWorker.Done(); communityService.Run(communityContext) })
	defer func() { stopCommunity(); communityWorker.Wait() }()

	readEventPublisher :=
		realtimeDomain.NewReadEventPublisher(realtimeHub)
	readRepository := readstateSqlite.NewRepository(db)
	readService := readstateDomain.NewService(
		readRepository,
		readEventPublisher,
	)
	readHandler := readstatehttp.NewHandler(readService)

	diagnosticsRepository := diagnosticsSqlite.NewRepository(db)
	maintenanceContext, stopMaintenance := context.WithCancel(context.Background())
	var maintenanceWorker sync.WaitGroup
	maintenanceWorker.Add(1)
	safego.Go("storage/maintenance", func() {
		defer maintenanceWorker.Done()
		timer := time.NewTicker(time.Minute)
		defer timer.Stop()
		for {
			ctx, cancel := context.WithTimeout(maintenanceContext, 2*time.Second)
			if err := diagnosticsRepository.Cleanup(ctx); err != nil && maintenanceContext.Err() == nil {
				log.Printf("diagnostic cleanup: %v", err)
			}
			if err := attachmentService.Cleanup(ctx); err != nil && maintenanceContext.Err() == nil {
				log.Printf("attachment cleanup: %v", err)
			}
			if err := messageRepository.PruneChanges(ctx); err != nil && maintenanceContext.Err() == nil {
				log.Printf("message journal cleanup: %v", err)
			}
			cancel()
			sessionCtx, cancelSessions := context.WithTimeout(maintenanceContext, 2*time.Second)
			if _, err := sessionRepository.DeleteExpired(sessionCtx, time.Now().Unix(), 256); err != nil && maintenanceContext.Err() == nil {
				log.Printf("expired session cleanup: %v", err)
			}
			cancelSessions()
			select {
			case <-maintenanceContext.Done():
				return
			case <-timer.C:
			}
		}
	})
	defer func() { stopMaintenance(); maintenanceWorker.Wait() }()
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
	webSocketHandler.SetOriginPatterns(corsManager.OriginPatterns)

	webrtcConfigHandler := webrtcconfighttp.NewHandler(
		clientICEURLs,
		clientICEUsername,
		clientICECredential,
	)

	mux := http.NewServeMux()
	communityService.RegisterRoutes(mux, accountHandler.RequireAuth)
	attachmentService.RegisterRoutes(mux, accountHandler.RequireAuth)
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
	avatarHandler.RegisterRoutes(mux, accountHandler.RequireAuth)
	recordingHandler.RegisterRoutes(mux, accountHandler.RequireAuth)
	notificationService.RegisterRoutes(mux, accountHandler.RequireAuth)

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
	corsManager.RegisterRoutes(
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
		Addr: net.JoinHostPort(listenAddress, port),
		Handler: corsManager.Middleware(
			antiAbuseGuard.ProtectHTTP(mux),
		),
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
