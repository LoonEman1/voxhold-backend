package sqlite

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"voxhold-backend/internal/testdb"
)

func TestDeleteExpiredIsBoundedAndPreservesRotatedSession(t *testing.T) {
	db := testdb.OpenFile(t)
	r := NewSessionRepository(db)
	ctx := context.Background()
	now := time.Now().Unix()
	for i := range 9 {
		hash := sha256.Sum256([]byte{byte(i)})
		expiry := now - 1
		if i == 8 {
			expiry = now + 1000
		}
		if err := r.Create(ctx, 1, hash[:], expiry); err != nil {
			t.Fatal(err)
		}
	}
	active := sha256.Sum256([]byte{8})
	rotated := sha256.Sum256([]byte("rotated"))
	if err := r.Rotate(ctx, active[:], rotated[:], now+2000); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int64{3, 3, 2, 0} {
		got, err := r.DeleteExpired(ctx, now, 3)
		if err != nil || got != want {
			t.Fatalf("deleted %d, want %d: %v", got, want, err)
		}
	}
	if _, err := r.FindActiveUserIDByTokenHash(ctx, rotated[:]); err != nil {
		t.Fatal("active rotated session lost:", err)
	}
	boundary := sha256.Sum256([]byte("boundary"))
	if err := r.Create(ctx, 1, boundary[:], now); err != nil {
		t.Fatal(err)
	}
	if n, err := r.DeleteExpired(ctx, now, 3); err != nil || n != 1 {
		t.Fatalf("boundary: %d %v", n, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := r.DeleteExpired(canceled, now, 3); err == nil {
		t.Fatal("canceled cleanup succeeded")
	}
}
