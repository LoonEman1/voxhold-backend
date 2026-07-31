package main

import (
	"context"
	"fmt"
	"log"
	"time"
	"voxhold-backend/internal/account"
	"voxhold-backend/internal/storage"
)

func main() {
	db, err := storage.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	log.Println("database is ready")

	userRepository := account.NewUserRepository(db)
	ctx := context.Background()

	// Уникальное имя, чтобы повторный запуск не нарушал UNIQUE.
	username := fmt.Sprintf("test_user_%d", time.Now().UnixNano())

	createdUser, err := userRepository.Create(
		ctx,
		username,
		"temporary-password-hash",
	)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf(
		"user created: id=%d username=%s created_at=%d",
		createdUser.ID,
		createdUser.Username,
		createdUser.CreatedAt,
	)

	foundUser, err := userRepository.FindByUsername(ctx, username)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf(
		"user found: id=%d username=%s created_at=%d",
		foundUser.ID,
		foundUser.Username,
		foundUser.CreatedAt,
	)
}
