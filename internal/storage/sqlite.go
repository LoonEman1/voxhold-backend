package storage

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func Open() (*sql.DB, error) {

	databasePath := os.Getenv("DATABASE_PATH")
	if databasePath == "" {
		databasePath = "data/voxhold.db"
	}

	params := url.Values{}
	params.Add("_pragma", "foreign_keys(1)")

	databaseURL := "file:" +
		filepath.ToSlash(databasePath) +
		"?" +
		params.Encode()

	db, err := sql.Open("sqlite", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}

	return db, nil
}
