package attachments

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
	"voxhold-backend/internal/recordings"
	"voxhold-backend/internal/testdb"
)

func TestCleanupSkipsThousandsOfAttachedFiles(t *testing.T) {
	db := testdb.OpenFile(t)
	dir := t.TempDir()
	t.Setenv("ATTACHMENT_DIRECTORY", dir)
	records, err := recordings.New(db, recordings.Config{Directory: t.TempDir()}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer records.Close()
	s, err := New(db, records, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	old := time.Now().Add(-25 * time.Hour)
	// A real referenced message keeps the foreign-key contract intact.
	result, err := tx.Exec(`INSERT INTO messages(channel_id,author_user_id,content,created_at) VALUES(2,2,'attached',unixepoch())`)
	if err != nil {
		t.Fatal(err)
	}
	messageID, _ := result.LastInsertId()
	for i := 1; i <= 5003; i++ {
		id := fmt.Sprintf("%032x", i)
		if err := s.root.WriteFile(id, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filepath.Join(dir, id), old, old); err != nil {
			t.Fatal(err)
		}
		var linked any = messageID
		if i > 5000 {
			linked = nil
		}
		if _, err := tx.Exec(`INSERT INTO attachments(id,channel_id,owner_user_id,filename,mime_type,category,size_bytes,message_id) VALUES(?,2,2,'x','text/plain','files',1,?)`, id, linked); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("5000 attached + 3 abandoned: %s", time.Since(started))
	for _, i := range []int{1, 5000} {
		if _, err := s.root.Stat(fmt.Sprintf("%032x", i)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 5001; i <= 5003; i++ {
		if _, err := s.root.Stat(fmt.Sprintf("%032x", i)); !os.IsNotExist(err) {
			t.Fatalf("orphan %d: %v", i, err)
		}
	}
	// A cancelled scan keeps its cursor; the next pass resumes and wraps.
	s.cleanupCursor = fmt.Sprintf("%032x", 2500)
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if err := s.Cleanup(cancelled); err != context.Canceled {
		t.Fatal(err)
	}
	if s.cleanupCursor == "" {
		t.Fatal("lost resume position")
	}
	if err := s.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.cleanupCursor != "" {
		t.Fatal("did not wrap at end")
	}
}
