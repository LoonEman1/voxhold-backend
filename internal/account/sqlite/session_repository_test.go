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

func TestTouchWritesActivityAtMostEveryFiveMinutes(t *testing.T) {
	db := testdb.Open(t)
	r := NewSessionRepository(db)
	ctx := context.Background()
	hash := sha256.Sum256([]byte("touch"))
	now := time.Now().Unix()
	if err := r.Create(ctx, 1, hash[:], now+3600); err != nil {
		t.Fatal(err)
	}
	lastUsed := func() int64 {
		t.Helper()
		var value int64
		if err := db.QueryRow(`SELECT COALESCE(last_used_at,0) FROM sessions WHERE token_hash=?`, hash[:]).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	for _, step := range []struct{ at, want int64 }{{now, now}, {now + 299, now}, {now + 301, now + 301}} {
		if err := r.Touch(ctx, hash[:], step.at); err != nil {
			t.Fatal(err)
		}
		if got := lastUsed(); got != step.want {
			t.Fatalf("touch at +%d: last_used_at=%d want %d", step.at-now, got, step.want)
		}
	}
	missing := sha256.Sum256([]byte("missing"))
	if err := r.Touch(ctx, missing[:], now); err != nil {
		t.Fatalf("unknown session: %v", err)
	}
}
