package recordings

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"voxhold-backend/internal/account"
)

func TestOwnerDeletesMediaPreviewAndDownloadTickets(t *testing.T) {
	s := setup(t)
	r := ready(t, s)
	r, err := s.SavePreview(ctx, r.ID, 1, previewImage(t))
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := s.Ticket(ctx, r.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Delete(ctx, r.ID, 2); !errors.Is(err, ErrNotFound) {
		t.Fatal("non-owner deleted recording", err)
	}
	var event Record
	s.publish = func(record Record) { event = record }
	user := int64(1)
	mux := http.NewServeMux()
	NewHandler(s).RegisterRoutes(mux, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(account.ContextWithUserID(r.Context(), user)))
		})
	})
	path := "/api/v1/recordings/" + r.ID
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("DELETE", path, nil))
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	if event.ID != r.ID || event.Status != "deleted" || event.PreviewVersion != nil || event.UpdatedAt <= r.UpdatedAt {
		t.Fatal("missing deletion event", event)
	}
	if _, err := s.root.Stat(r.ID + r.extension()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("video file survived deletion", err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM recording_previews WHERE recording_id=?", r.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("preview did not cascade", count, err)
	}
	if _, err := s.Get(ctx, r.ID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatal("metadata survived deletion", err)
	}
	if _, file, err := s.OpenDownload(ctx, ticket); !errors.Is(err, ErrNotFound) {
		if file != nil {
			file.Close()
		}
		t.Fatal("old ticket still works", err)
	}
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("GET", path+"/preview", nil))
	if response.Code != 404 {
		t.Fatal("deleted preview is accessible", response.Code)
	}
	storage, err := s.Storage(ctx, 1)
	if err != nil || storage.UsedBytes != 0 {
		t.Fatal("quota was not released", storage, err)
	}
	if _, err := s.Delete(ctx, r.ID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatal("repeat deletion", err)
	}
	active := start(t, s)
	if _, err := s.Delete(ctx, active.ID, 1); !errors.Is(err, ErrConflict) {
		t.Fatal("deleted active recording", err)
	}
}

func TestInstanceOwnerCanCleanAnotherMembersRecording(t *testing.T) {
	s := setup(t)
	r, err := s.Start(ctx, 1, 1, 2, "video/webm")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Stop(ctx, r.ID, 2, int64(len(webm)), 1000, false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Append(ctx, r.ID, 2, 0, webm); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Complete(ctx, r.ID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Delete(ctx, r.ID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatal("ordinary deletion must respect authorship", err)
	}
	if _, err = s.DeleteForOwner(ctx, r.ID, 2); !errors.Is(err, ErrForbidden) {
		t.Fatal("member used owner cleanup", err)
	}
	if _, err = s.DeleteForOwner(ctx, r.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, r.ID, 2); !errors.Is(err, ErrNotFound) {
		t.Fatal("server copy survived cleanup", err)
	}
}
