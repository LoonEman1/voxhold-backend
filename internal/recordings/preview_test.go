package recordings

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"voxhold-backend/internal/account"
)

func previewImage(t *testing.T) []byte {
	t.Helper()
	source := image.NewRGBA(image.Rect(0, 0, 640, 360))
	for y := range 360 {
		for x := range 640 {
			source.Set(x, y, color.RGBA{R: 25, G: 180, B: 100, A: 255})
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, source); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestRecordingPreviewLifecycleAndAccess(t *testing.T) {
	s := setup(t)
	record := ready(t, s)
	if record.PreviewVersion != nil {
		t.Fatal("new recording unexpectedly has a preview")
	}
	user := int64(1)
	mux := http.NewServeMux()
	NewHandler(s).RegisterRoutes(mux, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(account.ContextWithUserID(r.Context(), user)))
		})
	})
	path := "/api/v1/recordings/" + record.ID + "/preview"
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
	if response.Code != 404 {
		t.Fatal(response.Code)
	}
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("PUT", path, bytes.NewReader(previewImage(t))))
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	updated, err := s.Get(ctx, record.ID, 1)
	if err != nil || updated.PreviewVersion == nil || updated.UpdatedAt <= record.UpdatedAt {
		t.Fatal(updated, err)
	}
	listed, err := s.List(ctx, 1, 2)
	if err != nil || len(listed) != 1 || listed[0].PreviewVersion == nil {
		t.Fatal(listed, err)
	}
	user = 2
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
	if response.Code != 200 || response.Header().Get("Content-Type") != "image/jpeg" || response.Body.Len() > 65536 {
		t.Fatal(response.Code, response.Header(), response.Body.Len())
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(response.Body.Bytes()))
	if err != nil || format != "jpeg" || config.Width != 320 || config.Height != 180 {
		t.Fatal(config, format, err)
	}
	etag := response.Header().Get("ETag")
	conditional := httptest.NewRequest("GET", path, nil)
	conditional.Header.Set("If-None-Match", etag)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, conditional)
	if response.Code != 304 || response.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatal(response.Code, response.Header())
	}
	if _, err := s.SavePreview(ctx, record.ID, 2, previewImage(t)); !errors.Is(err, ErrNotFound) {
		t.Fatal("non-owner uploaded a preview", err)
	}
	if _, err := s.db.Exec("DELETE FROM server_members WHERE server_id=1 AND user_id=2"); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, conditional)
	if response.Code != 404 {
		t.Fatal("conditional cache bypassed revoked access", response.Code)
	}
	user = 1
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("PUT", path, bytes.NewReader([]byte("not an image"))))
	if response.Code != 400 {
		t.Fatal(response.Code)
	}
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("PUT", path, bytes.NewReader(make([]byte, MaxPreviewUpload+1))))
	if response.Code != 413 {
		t.Fatal(response.Code)
	}
	current, _ := s.Get(ctx, record.ID, 1)
	if current.PreviewVersion == nil || *current.PreviewVersion != *updated.PreviewVersion {
		t.Fatal("invalid image replaced the preview")
	}
	storage, err := s.Storage(ctx, 1)
	if err != nil || storage.UsedBytes <= record.SizeBytes {
		t.Fatal("preview not counted towards quota", storage, err)
	}
	s.now = func() time.Time { return time.Unix(record.ExpiresAt+1, 0) }
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, conditional)
	if response.Code != 404 {
		t.Fatal("expired preview is visible", response.Code)
	}
	if err := s.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM recording_previews").Scan(&count); err != nil || count != 0 {
		t.Fatal("orphaned preview", count, err)
	}
}

func TestPreviewValidationAndQuota(t *testing.T) {
	s := setup(t)
	r := start(t, s)
	data := previewImage(t)
	if _, err := s.SavePreview(ctx, r.ID, 1, data); !errors.Is(err, ErrConflict) {
		t.Fatal("preview accepted before the recording is ready", err)
	}
	if _, err := s.Stop(ctx, r.ID, 1, 0, 0, true); err != nil {
		t.Fatal(err)
	}
	r = ready(t, s)
	s.quota.Store(r.SizeBytes)
	if _, err := s.SavePreview(ctx, r.ID, 1, data); !errors.Is(err, ErrLimit) {
		t.Fatal("preview exceeded quota", err)
	}
	var large bytes.Buffer
	if err := png.Encode(&large, image.NewRGBA(image.Rect(0, 0, 1281, 720))); err != nil {
		t.Fatal(err)
	}
	if _, err := optimizePreview(large.Bytes()); !errors.Is(err, ErrPreview) {
		t.Fatal("oversized dimensions accepted", err)
	}
}
