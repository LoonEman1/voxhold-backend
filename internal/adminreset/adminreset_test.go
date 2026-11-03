package adminreset

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"voxhold-backend/internal/testdb"
)

func TestResetPasswordReplacesHashAndDropsSessions(t *testing.T) {
	db := testdb.Open(t)
	old, _ := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.MinCost)
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash) VALUES(50,'owner2',?)`, string(old)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions(user_id,token_hash,expires_at) VALUES(50,randomblob(32),unixepoch()+1000)`); err != nil {
		t.Fatal(err)
	}
	password, err := ResetPassword(context.Background(), db, "owner2")
	if err != nil || len(password) != passwordLength {
		t.Fatalf("password=%q err=%v", password, err)
	}
	var hash string
	var sessions int
	if err = db.QueryRow(`SELECT password_hash FROM users WHERE id=50`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("old-password")) == nil {
		t.Fatal("old password still valid")
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		t.Fatal("new password invalid")
	}
	if err = db.QueryRow(`SELECT count(*) FROM sessions WHERE user_id=50`).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatalf("sessions=%d err=%v", sessions, err)
	}
}

func TestResetPasswordRejectsMissingAndDeletedUsers(t *testing.T) {
	db := testdb.Open(t)
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash,deleted_at) VALUES(51,'gone','x',1)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nobody", "gone"} {
		if _, err := ResetPassword(context.Background(), db, name); !errors.Is(err, ErrUserNotFound) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
