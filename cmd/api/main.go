package main

import (
	"log"
	"net/http"
	"os"
	"time"
	"voxhold-backend/internal/account"
	"voxhold-backend/internal/voice"

	accounthttp "voxhold-backend/internal/account/http"
	accountSqlite "voxhold-backend/internal/account/sqlite"
	serverDomain "voxhold-backend/internal/server"
	serverhttp "voxhold-backend/internal/server/http"
	serverSqlite "voxhold-backend/internal/server/sqlite"
	"voxhold-backend/internal/storage"

	channelDomain "voxhold-backend/internal/channel"
	channelhttp "voxhold-backend/internal/channel/http"
	channelSqlite "voxhold-backend/internal/channel/sqlite"

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

	realtimeDomain "voxhold-backend/internal/realtime"
)

func main() {
	db, err := storage.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	log.Println("database is ready")

	realtimeHub := realtimeDomain.NewHub()
	voiceConfig, err := voice.NewConfig(
		os.Getenv("WEBRTC_UDP_PORT"),
		os.Getenv("WEBRTC_MAX_PARTICIPANTS"),
		os.Getenv("WEBRTC_PUBLIC_IP"),
		os.Getenv("WEBRTC_ICE_SERVERS"),
		os.Getenv("WEBRTC_ICE_USERNAME"),
		os.Getenv("WEBRTC_ICE_CREDENTIAL"),
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
		"WebRTC audio is listening on UDP port %d",
		voiceConfig.UDPPort,
	)

	serverEventPublisher :=
		realtimeDomain.NewServerEventPublisher(realtimeHub)
	channelEventPublisher :=
		realtimeDomain.NewChannelEventPublisher(realtimeHub)
	inviteEventPublisher :=
		realtimeDomain.NewInviteEventPublisher(realtimeHub)

	userRepository := accountSqlite.NewUserRepository(db)
	sessionRepository := accountSqlite.NewSessionRepository(db)

	accountService := account.NewService(
		userRepository,
		sessionRepository,
		realtimeHub,
	)
	accountHandler := accounthttp.NewHandler(accountService)

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

	webSocketHandler := realtimehttp.NewHandler(
		accountService,
		channelService,
		serverService,
		readService,
		voiceManager,
		realtimeHub,
	)

	mux := http.NewServeMux()
	accountHandler.RegisterRoutes(mux)
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

	webSocketHandler.RegisterRoutes(mux)

	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("server started on: %s", server.Addr)

	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
