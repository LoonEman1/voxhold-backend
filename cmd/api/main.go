package main

import (
	"context"
	"fmt"
	"log"
	"time"
	"voxhold-backend/internal/account"
	"voxhold-backend/internal/storage"

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

	input := account.RegisterInput{
		Username:        fmt.Sprintf("test_user_%d", time.Now().UnixNano()),
		Password:        "password123",
		PasswordConfirm: "password123",
	}

	user, err := accountService.Register(
		context.Background(),
		input,
	)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf(
		"user registered: id=%d username=%s created_at=%d",
		user.ID,
		user.Username,
		user.CreatedAt,
	)

	log.Printf("password hash: %s", user.PasswordHash)
}
