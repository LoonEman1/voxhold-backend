package attachments

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/recordings"
	"voxhold-backend/internal/testdb"
)

func TestCleanupAndSendArbitrateBeforeRemovingContent(t *testing.T) {
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
	repo := messagesqlite.NewRepository(db)
	ctx := context.Background()
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("%032x", i+1)
		if err := s.root.WriteFile(id, []byte("file"), 0600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-25 * time.Hour)
		if err := os.Chtimes(filepath.Join(dir, id), old, old); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO attachments(id,channel_id,owner_user_id,filename,mime_type,category,size_bytes) VALUES(?,2,2,'file.txt','text/plain','files',4)`, id); err != nil {
			t.Fatal(err)
		}
		var sendErr, cleanupErr error
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, sendErr = repo.CreateReferenced(ctx, 1, 2, 2, message.CreateInput{Content: "race", ClientMessageID: fmt.Sprintf("race-%d", i), AttachmentIDs: []string{id}}, nil)
		}()
		go func() { defer wg.Done(); <-start; cleanupErr = s.Cleanup(ctx) }()
		close(start)
		wg.Wait()
		if cleanupErr != nil {
			t.Fatal(cleanupErr)
		}
		if sendErr != nil && !errors.Is(sendErr, message.ErrReplyInvalid) {
			t.Fatal(sendErr)
		}
		f, fileErr := s.root.Open(id)
		if f != nil {
			f.Close()
		}
		var linked, deleted bool
		if err := db.QueryRow(`SELECT message_id IS NOT NULL,deleted_at IS NOT NULL FROM attachments WHERE id=?`, id).Scan(&linked, &deleted); err != nil {
			t.Fatal(err)
		}
		if sendErr == nil && (!linked || deleted || fileErr != nil) {
			t.Fatalf("successful send lost file: linked=%t deleted=%t err=%v", linked, deleted, fileErr)
		}
		if sendErr != nil && (!deleted || !errors.Is(fileErr, os.ErrNotExist)) {
			t.Fatalf("cleanup winner: deleted=%t file=%v", deleted, fileErr)
		}
	}
}
