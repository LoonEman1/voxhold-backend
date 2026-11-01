// Command voxhold-admin holds operator-only maintenance commands.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"voxhold-backend/internal/adminreset"
	"voxhold-backend/internal/storage"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "reset-password" {
		fmt.Fprintln(os.Stderr, "usage: voxhold-admin reset-password --username <name>")
		os.Exit(2)
	}
	flags := flag.NewFlagSet("reset-password", flag.ExitOnError)
	username := flags.String("username", "", "account to reset")
	_ = flags.Parse(os.Args[2:])
	if *username == "" {
		fmt.Fprintln(os.Stderr, "--username is required")
		os.Exit(2)
	}
	db, err := storage.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	password, err := adminreset.ResetPassword(context.Background(), db, *username)
	if err != nil {
		db.Close()
		log.Fatal(err)
	}
	fmt.Println(password)
	fmt.Fprintln(os.Stderr, "New password printed once; all sessions of the user were revoked.")
}
