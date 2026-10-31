package attachments

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
	"voxhold-backend/internal/account"
	"voxhold-backend/internal/recordings"
	"voxhold-backend/internal/testdb"
)

type uploadGate struct {
	*bytes.Reader
	started chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (r *uploadGate) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started); <-r.release })
	return r.Reader.Read(p)
}

func TestOrdinaryUploadsAllowThreeAccountsButOnlyTwoPerAccount(t *testing.T) {
	db := testdb.OpenFile(t)
	t.Setenv("ATTACHMENT_DIRECTORY", t.TempDir())
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash) VALUES(4,'four','hash');INSERT INTO server_members(server_id,user_id,role) VALUES(1,4,'member')`); err != nil {
		t.Fatal(err)
	}
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
	mux := http.NewServeMux()
	s.RegisterRoutes(mux, func(h http.Handler) http.Handler { return h })
	release := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(release) }) }
	var wg sync.WaitGroup
	defer func() { stop(); wg.Wait() }()
	results := make(chan int, 3)
	started := make([]chan struct{}, 3)
	for i, uid := range []int64{1, 2, 4} {
		started[i] = make(chan struct{})
		body := &uploadGate{Reader: bytes.NewReader([]byte("text")), started: started[i], release: release}
		wg.Add(1)
		go func(i int, uid int64) {
			defer wg.Done()
			r := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/channels/2/attachments/%032x?filename=x.txt", i+1), body)
			r = r.WithContext(account.ContextWithUserID(r.Context(), uid))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			results <- w.Code
		}(i, uid)
	}
	for _, ready := range started {
		select {
		case <-ready:
		case status := <-results:
			t.Fatalf("upload rejected before body: %d", status)
		case <-time.After(2 * time.Second):
			t.Fatal("third account could not enter upload")
		}
	}
	stop()
	wg.Wait()
	for range started {
		if got := <-results; got != 200 {
			t.Fatal(got)
		}
	}
	if len(s.userUploads) != 0 {
		t.Fatal("user counters leaked")
	}
	if !s.acquireUserUpload(2) || !s.acquireUserUpload(2) || s.acquireUserUpload(2) {
		t.Fatal("user limit is not two")
	}
	r := httptest.NewRequest("PUT", "/api/v1/channels/2/attachments/99999999999999999999999999999999?filename=x.txt", bytes.NewReader([]byte("text")))
	r = r.WithContext(account.ContextWithUserID(r.Context(), 2))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 429 || w.Header().Get("Retry-After") != "2" {
		t.Fatal(w.Code, w.Header())
	}
	s.releaseUserUpload(2)
	s.releaseUserUpload(2)
	if len(s.userUploads) != 0 {
		t.Fatal("idle user retained")
	}
}

func TestImageProcessingWaitsAndHonoursCancellation(t *testing.T) {
	s := &Service{imageSlots: make(chan struct{}, 2)}
	s.imageSlots <- struct{}{}
	s.imageSlots <- struct{}{}
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	ready := make(chan error, 1)
	go func() {
		_, _, _, _, err := s.compressUpload(context.Background(), bytes.NewReader(data.Bytes()))
		ready <- err
	}()
	select {
	case err := <-ready:
		t.Fatalf("did not wait: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	<-s.imageSlots
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("image did not resume")
	}
	s.imageSlots <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, _, err := s.compressUpload(ctx, bytes.NewReader(data.Bytes())); err != context.Canceled {
		t.Fatal(err)
	}
	<-s.imageSlots
	<-s.imageSlots
}
