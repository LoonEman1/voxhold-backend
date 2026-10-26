package recordings

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"voxhold-backend/internal/testdb"
)

var ctx = context.Background()
var webm = append([]byte{0x1a, 0x45, 0xdf, 0xa3}, bytes.Repeat([]byte{1}, 100)...)

func setup(t *testing.T) *Service {
	t.Helper()
	s, err := New(testdb.Open(t), Config{Directory: t.TempDir()}, func(user, server, channel int64) bool {
		return user < 3 && server == 1 && (channel == 1 || channel == 4)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func start(t *testing.T, s *Service) Record {
	t.Helper()
	r, e := s.Start(ctx, 1, 1, 1, "video/webm")
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func ready(t *testing.T, s *Service) Record {
	t.Helper()
	r := start(t, s)
	var e error
	r, e = s.Stop(ctx, r.ID, 1, int64(len(webm)), 1000, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Append(ctx, r.ID, 1, 0, webm); e != nil {
		t.Fatal(e)
	}
	r, e = s.Complete(ctx, r.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestResumableUploadAndNativeAttachment(t *testing.T) {
	s := setup(t)
	r := start(t, s)
	if _, e := s.Start(ctx, 1, 1, 2, "video/webm"); !errors.Is(e, ErrConflict) {
		t.Fatalf("duplicate recorder: %v", e)
	}
	if _, e := s.Stop(ctx, r.ID, 2, 100, 1, false); !errors.Is(e, ErrConflict) {
		t.Fatalf("other owner stop: %v", e)
	}
	r, e := s.Stop(ctx, r.ID, 1, int64(len(webm)), 1000, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Append(ctx, r.ID, 2, 0, webm); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if _, e = s.Append(ctx, r.ID, 1, 0, webm[:40]); e != nil {
		t.Fatal(e)
	}
	if offset, e := s.Append(ctx, r.ID, 1, 0, webm[:40]); offset != 40 || !errors.Is(e, ErrConflict) {
		t.Fatalf("retry offset %d: %v", offset, e)
	}
	if _, e = s.Complete(ctx, r.ID, 1); !errors.Is(e, ErrConflict) {
		t.Fatalf("partial complete: %v", e)
	}
	if offset, e := s.Offset(ctx, r.ID, 1); offset != 40 || e != nil {
		t.Fatal(offset, e)
	}
	if _, e = s.Append(ctx, r.ID, 1, 40, webm[40:]); e != nil {
		t.Fatal(e)
	}
	r, e = s.Complete(ctx, r.ID, 1)
	if e != nil || r.Status != "ready" {
		t.Fatal(r, e)
	}
	if _, e = s.Complete(ctx, r.ID, 1); e != nil {
		t.Fatal(e)
	}
	ticket, e := s.Ticket(ctx, r.ID, 2)
	if e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	NewHandler(s).RegisterRoutes(mux, func(h http.Handler) http.Handler { return h })
	request := httptest.NewRequest("GET", "/api/v1/recording-downloads/"+ticket, nil)
	request.Header.Set("Range", "bytes=4-11")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != 206 || !bytes.Equal(response.Body.Bytes(), webm[4:12]) || response.Header().Get("Content-Disposition") != "attachment; filename=\""+r.Filename()+"\"" {
		t.Fatalf("download: %d %s %q", response.Code, response.Header(), response.Body.Bytes())
	}
	if _, e = s.Ticket(ctx, r.ID, 3); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("DELETE FROM server_members WHERE server_id=1 AND user_id=2"); e != nil {
		t.Fatal(e)
	}
	if _, f, e := s.OpenDownload(ctx, ticket); !errors.Is(e, ErrNotFound) {
		if f != nil {
			f.Close()
		}
		t.Fatalf("revoked ticket: %v", e)
	}
}
func TestQuotaLargeLocalOnlyAndHeaderValidation(t *testing.T) {
	s := setup(t)
	s.config.QuotaBytes = MaxFileBytes
	r := start(t, s)
	if r, e := s.Stop(ctx, r.ID, 1, MaxFileBytes+1, 1000, false); e != nil || r.Status != "local_only" || r.SizeBytes != 0 {
		t.Fatal(r, e)
	}
	r = start(t, s)
	if _, e := s.Stop(ctx, r.ID, 1, MaxFileBytes, 1000, false); e != nil {
		t.Fatal(e)
	}
	second, e := s.Start(ctx, 1, 4, 2, "video/mp4")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Stop(ctx, second.ID, 2, 12, 1, false); !errors.Is(e, ErrLimit) {
		t.Fatal(e)
	}
	if r, e := s.Stop(ctx, second.ID, 2, 12, 1, true); e != nil || r.Status != "local_only" {
		t.Fatal(r, e)
	}
	if _, e = s.Stop(ctx, r.ID, 1, 12, 1, true); e != nil {
		t.Fatal(e)
	}
	r = start(t, s)
	if _, e = s.Stop(ctx, r.ID, 1, 12, 1, false); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Append(ctx, r.ID, 1, 0, []byte("invalid-data")); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Complete(ctx, r.ID, 1); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
}
func TestCleanupLeaseExpirationAndOrphans(t *testing.T) {
	s := setup(t)
	r := ready(t, s)
	ticket, e := s.Ticket(ctx, r.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	s.now = func() time.Time { return now.Add(6 * time.Minute) }
	if _, f, e := s.OpenDownload(ctx, ticket); !errors.Is(e, ErrNotFound) {
		if f != nil {
			f.Close()
		}
		t.Fatal(e)
	}
	active := start(t, s)
	if e = s.Cleanup(ctx); e != nil {
		t.Fatal(e)
	}
	if current, e := s.Get(ctx, active.ID, 1); e != nil || current.Status != "failed" {
		t.Fatal(current, e)
	}
	if e = os.WriteFile(s.config.Directory+"/11111111111111111111111111111111.part", []byte("orphan"), 0600); e != nil {
		t.Fatal(e)
	}
	s.now = func() time.Time { return now.Add(25 * time.Hour) }
	if e = s.Cleanup(ctx); e != nil {
		t.Fatal(e)
	}
	files, e := os.ReadDir(s.config.Directory)
	if e != nil || len(files) != 0 {
		t.Fatal(files, e)
	}
	if _, e = s.Get(ctx, r.ID, 1); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}
func TestCompleteRecoversRenameBeforeMetadataCommit(t *testing.T) {
	s := setup(t)
	r := start(t, s)
	if _, e := s.Stop(ctx, r.ID, 1, int64(len(webm)), 1, false); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Append(ctx, r.ID, 1, 0, webm); e != nil {
		t.Fatal(e)
	}
	if e := s.root.Rename(r.ID+".part", r.ID+".webm"); e != nil {
		t.Fatal(e)
	}
	if offset, e := s.Offset(ctx, r.ID, 1); e != nil || offset != int64(len(webm)) {
		t.Fatal(offset, e)
	}
	if _, e := s.Complete(ctx, r.ID, 1); e != nil {
		t.Fatal(e)
	}
	ticket, e := s.Ticket(ctx, r.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	_, f, e := s.OpenDownload(ctx, ticket)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	data, e := io.ReadAll(f)
	if e != nil || !bytes.Equal(data, webm) {
		t.Fatal(e)
	}
}
func TestConcurrentStartHasOneWinner(t *testing.T) {
	s := setup(t)
	var won atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 10; i++ {
		group.Go(func() {
			if _, e := s.Start(ctx, 1, 1, 1, "video/webm"); e == nil {
				won.Add(1)
			}
		})
	}
	group.Wait()
	if won.Load() != 1 {
		t.Fatal(won.Load())
	}
}

func TestDownloadOutlivesGlobalHTTPWriteTimeout(t *testing.T) {
	s := setup(t)
	r := start(t, s)
	payload := append(append([]byte{}, webm...), bytes.Repeat([]byte{0}, 512<<10)...)
	if _, err := s.Stop(ctx, r.ID, 1, int64(len(payload)), 1, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, r.ID, 1, 0, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Complete(ctx, r.ID, 1); err != nil {
		t.Fatal(err)
	}
	ticket, err := s.Ticket(ctx, r.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	NewHandler(s).RegisterRoutes(mux, func(h http.Handler) http.Handler { return h })
	server := httptest.NewUnstartedServer(mux)
	server.Config.WriteTimeout = 10 * time.Millisecond
	server.Start()
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/api/v1/recording-downloads/" + ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("streamed download truncated at %d bytes: %v", len(data), err)
	}
}
