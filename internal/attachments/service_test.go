package attachments

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"voxhold-backend/internal/account"
	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/recordings"
	"voxhold-backend/internal/testdb"
)

func TestUploadCompressionAuthorizationRetryQuotaAndCleanup(t *testing.T) {
	db := testdb.Open(t)
	t.Setenv("ATTACHMENT_DIRECTORY", t.TempDir())
	recordingService, err := recordings.New(db, recordings.Config{Directory: t.TempDir()}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer recordingService.Close()
	s, err := New(db, recordingService, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mux := http.NewServeMux()
	s.RegisterRoutes(mux, func(h http.Handler) http.Handler { return h })
	call := func(uid int64, method, url string, data []byte, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, url, bytes.NewReader(data))
		r = r.WithContext(account.ContextWithUserID(r.Context(), uid))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s => %d want %d: %s", method, url, w.Code, want, w.Body.String())
		}
		return w
	}
	id := "0123456789abcdef0123456789abcdef"
	url := "/api/v1/channels/2/attachments/" + id + "?filename=screen.png"
	img := image.NewNRGBA(image.Rect(0, 0, 3000, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3000; x++ {
			img.Set(x, y, color.RGBA{R: 200, A: 255})
		}
	}
	var body bytes.Buffer
	if err = png.Encode(&body, img); err != nil {
		t.Fatal(err)
	}
	call(3, "PUT", url, body.Bytes(), 403)
	w := call(2, "PUT", url, body.Bytes(), 200)
	var a message.Attachment
	if err = json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if a.MIMEType != "image/jpeg" {
		t.Fatal(a)
	}
	call(2, "PUT", url, body.Bytes(), 200)
	// A second uploader must not bypass the metadata key using a case variant
	// of the same filesystem name (Windows and common macOS volumes).
	call(1, "PUT", "/api/v1/channels/2/attachments/"+strings.ToUpper(id)+"?filename=overwrite.png", body.Bytes(), 400)
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM attachments`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retry duplicated: %d %v", count, err)
	}
	// Staged uploads are visible only to the uploader, even within the channel.
	call(1, "GET", "/api/v1/attachments/"+id+"/content", nil, 404)
	repo := messagesqlite.NewRepository(db)
	post, err := repo.CreateReferenced(context.Background(), 1, 2, 2, message.CreateInput{Content: "file", AttachmentIDs: []string{id}, ClientMessageID: "file-send"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(post.Attachments) != 1 {
		t.Fatal(post)
	}
	content := call(1, "GET", "/api/v1/attachments/"+id+"/content", nil, 200)
	config, _, err := image.DecodeConfig(bytes.NewReader(content.Body.Bytes()))
	if err != nil || config.Width > 2560 {
		t.Fatalf("compression: %v %v", config, err)
	}
	call(3, "GET", "/api/v1/attachments/"+id+"/content", nil, 404)
	call(2, "GET", "/api/v1/instance/storage", nil, 403)
	if _, err = db.Exec(`UPDATE storage_budgets SET quota_bytes=0 WHERE category='files'`); err != nil {
		t.Fatal(err)
	}
	call(2, "PUT", "/api/v1/channels/2/attachments/11111111111111111111111111111111?filename=x.txt", []byte("hello"), 507)
	if _, err = db.Exec(`UPDATE attachments SET created_at=? WHERE id=?`, time.Now().Unix()-100, id); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"category": "images", "before": time.Now().Unix() - 1})
	preview := call(1, "POST", "/api/v1/instance/storage/preview", payload, 200)
	var p Plan
	if err = json.Unmarshal(preview.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Count != 1 || p.Bytes != a.SizeBytes {
		t.Fatal(p)
	}
	// New uploads after preview must survive that exact deletion plan.
	call(2, "PUT", "/api/v1/channels/2/attachments/22222222222222222222222222222222?filename=new.png", body.Bytes(), 200)
	payload, _ = json.Marshal(map[string]string{"token": p.Token})
	call(1, "POST", "/api/v1/instance/storage/cleanup", payload, 200)
	call(1, "POST", "/api/v1/instance/storage/cleanup", payload, 409)
	call(1, "GET", "/api/v1/attachments/"+id+"/content", nil, 410)
	call(2, "GET", "/api/v1/attachments/22222222222222222222222222222222/content", nil, 200)
	page, err := repo.ListByChannelID(context.Background(), 1, 2, 1, nil, 20)
	if err != nil || len(page) != 1 || !page[0].Attachments[0].Deleted {
		t.Fatalf("placeholder: %v %v", page, err)
	}
}
func TestInvalidImageDimensionsAndCorruption(t *testing.T) {
	if _, _, err := compressImage([]byte("broken image")); err == nil {
		t.Fatal("accepted corrupt image")
	}
	// DecodeConfig rejects a decompression bomb before allocating its pixels.
	var b bytes.Buffer
	img := image.NewGray(image.Rect(0, 0, 6000, 6000))
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	if _, _, err := compressImage(b.Bytes()); err == nil {
		t.Fatal("accepted oversized image")
	}
}
