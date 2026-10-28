// Command qa-community creates disposable data for the community browser tests.
// It refuses to overwrite an existing database. All credentials are public fixtures.
package main

import (
	"context"
	"database/sql"
	"golang.org/x/crypto/bcrypt"
	"log"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"voxhold-backend/internal/instancebootstrap"
)

func main() {
	directory := os.Getenv("QA_DIRECTORY")
	if directory == "" {
		log.Fatal("QA_DIRECTORY is required; use an isolated directory for fixture data")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		log.Fatal(err)
	}
	path := filepath.Join(directory, "voxhold.db")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		log.Fatal("seed requires a fresh QA database")
	}
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	files, err := filepath.Glob("migrations/*.up.sql")
	if err != nil {
		log.Fatal(err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			log.Fatal(err)
		}
		if _, err = db.Exec(string(data)); err != nil {
			log.Fatal(file, err)
		}
	}
	if _, err = instancebootstrap.Ensure(context.Background(), db, instancebootstrap.Config{Username: "qaowner", Password: "Community2.3-Test!", InstanceName: "QA Community 2.3"}); err != nil {
		log.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("Community2.3-Test!"), bcrypt.MinCost)
	if err != nil {
		log.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO users(id,username,password_hash) VALUES(2,'qamember',?),(3,'qafriend',?);INSERT INTO server_members(server_id,user_id,role) VALUES(1,2,'member'),(1,3,'member');INSERT INTO channels(id,server_id,name,kind,created_by) VALUES(1,1,'qa-chat','text',1),(2,1,'qa-voice','voice',1);INSERT INTO messages(channel_id,author_user_id,content) VALUES(1,2,'Обсудим накопительный релиз');`, string(hash), string(hash)); err != nil {
		log.Fatal(err)
	}
	log.Print("fresh QA data ready")
}
