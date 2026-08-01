package main

import (
	"log"
	"net/http"
	"os"
	"time"
	"voxhold-backend/internal/account"
	"voxhold-backend/internal/storage"

	accounthttp "voxhold-backend/internal/account/http"
	accountSqlite "voxhold-backend/internal/account/sqlite"
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
	mux := http.NewServeMux()
	accountHandler.RegisterRoutes(mux)

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
