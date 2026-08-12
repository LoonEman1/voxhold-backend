package main

import (
	"context"
	"log"
	"os"

	"voxhold-backend/internal/instancebootstrap"
	"voxhold-backend/internal/storage"
)

func main() {
	db, err := storage.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	result, err := instancebootstrap.Ensure(
		context.Background(),
		db,
		instancebootstrap.Config{
			Username:     os.Getenv("BOOTSTRAP_USERNAME"),
			Password:     os.Getenv("BOOTSTRAP_PASSWORD"),
			InstanceName: os.Getenv("INSTANCE_NAME"),
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	if !result.Created {
		log.Println("Voxhold instance is already initialized")
		return
	}

	log.Printf("Voxhold instance created; owner username: %s", result.Username)
	if result.PasswordGenerated {
		log.Printf("generated one-time owner password: %s", result.Password)
		log.Println("save this password now; it will not be printed again")
	}
}
