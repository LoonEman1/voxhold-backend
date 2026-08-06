package main

import (
	"log"
	"net/http"
	"os"
	"time"
	"voxhold-backend/internal/account"

	accounthttp "voxhold-backend/internal/account/http"
	accountSqlite "voxhold-backend/internal/account/sqlite"
	serverDomain "voxhold-backend/internal/server"
	serverhttp "voxhold-backend/internal/server/http"
	serverSqlite "voxhold-backend/internal/server/sqlite"
	"voxhold-backend/internal/storage"
)

func main() {
	db, err := storage.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	log.Println("database is ready")

	userRepository := accountSqlite.NewUserRepository(db)
	sessionRepository := accountSqlite.NewSessionRepository(db)

	accountService := account.NewService(userRepository, sessionRepository)

	accountHandler := accounthttp.NewHandler(accountService)

	serverRepository := serverSqlite.NewRepository(db)
	serverService := serverDomain.NewService(serverRepository)
	serverHandler := serverhttp.NewHandler(serverService)

	mux := http.NewServeMux()
	accountHandler.RegisterRoutes(mux)
	serverHandler.RegisterRoutes(
		mux,
		accountHandler.RequireAuth,
	)

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
