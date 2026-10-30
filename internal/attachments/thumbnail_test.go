package attachments

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"voxhold-backend/internal/account"
	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/recordings"
	"voxhold-backend/internal/testdb"
)

func TestThumbnailSharesContentACLAndHEADIsLightweight(t *testing.T) {
	db := testdb.Open(t)
	t.Setenv("ATTACHMENT_DIRECTORY", t.TempDir())
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
	call := func(uid int64, method, url string, data []byte, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, url, bytes.NewReader(data))
		r = r.WithContext(account.ContextWithUserID(r.Context(), uid))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s =%d want %d: %s", method, url, w.Code, want, w.Body.String())
		}
		return w
	}
	id := "0123456789abcdef0123456789abcdef"
	var body bytes.Buffer
	if err := png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 1000, 600))); err != nil {
		t.Fatal(err)
	}
	call(2, "PUT", "/api/v1/channels/2/attachments/"+id+"?filename=test.png", body.Bytes(), 200)
	call(1, "GET", "/api/v1/attachments/"+id+"/thumbnail", nil, 404)
	call(3, "GET", "/api/v1/attachments/"+id+"/thumbnail", nil, 404)
	thumb := call(2, "GET", "/api/v1/attachments/"+id+"/thumbnail", nil, 200)
	config, typ, err := image.DecodeConfig(bytes.NewReader(thumb.Body.Bytes()))
	if err != nil || typ != "jpeg" || config.Width > 480 || config.Height > 480 {
		t.Fatalf("thumbnail=%+v %s %v", config, typ, err)
	}
	if _, err := messagesqlite.NewRepository(db).CreateReferenced(context.Background(), 1, 2, 2, message.CreateInput{Content: "file", AttachmentIDs: []string{id}}, nil); err != nil {
		t.Fatal(err)
	}
	call(1, "GET", "/api/v1/attachments/"+id+"/thumbnail", nil, 200)
	head := call(1, "HEAD", "/api/v1/attachments/"+id+"/content", nil, 200)
	if head.Body.Len() != 0 || head.Header().Get("Content-Length") == "" {
		t.Fatalf("HEAD body=%d headers=%v", head.Body.Len(), head.Header())
	}
	for _, typ := range []string{"text/html", "image/svg+xml", "application/xhtml+xml", "image/custom"} {
		if _, err := db.Exec(`UPDATE attachments SET mime_type=? WHERE id=?`, typ, id); err != nil {
			t.Fatal(err)
		}
		content := call(1, "GET", "/api/v1/attachments/"+id+"/content", nil, 200)
		if !strings.HasPrefix(content.Header().Get("Content-Disposition"), "attachment;") || content.Header().Get("Content-Security-Policy") != "default-src 'none'; sandbox" || content.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("active MIME %s headers=%v", typ, content.Header())
		}
	}
	if _, err := db.Exec(`UPDATE attachments SET deleted_at=unixepoch() WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	call(1, "GET", "/api/v1/attachments/"+id+"/thumbnail", nil, 410)
	call(1, "HEAD", "/api/v1/attachments/"+id+"/content", nil, 410)
}
