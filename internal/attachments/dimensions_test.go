package attachments

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestImageDimensionsSurviveCopyAndLegacyRowsOmitFields(t *testing.T) {
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
	id, copyID := "0123456789abcdef0123456789abcdef", "1123456789abcdef0123456789abcdef"
	var body bytes.Buffer
	if err := png.Encode(&body, image.NewNRGBA(image.Rect(0, 0, 80, 60))); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("PUT", "/api/v1/channels/2/attachments/"+id+"?filename=image.png", &body)
	r = r.WithContext(account.ContextWithUserID(r.Context(), 2))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	repo := messagesqlite.NewRepository(db)
	post, err := repo.CreateReferenced(context.Background(), 1, 2, 2, message.CreateInput{Content: "image", AttachmentIDs: []string{id}, ClientMessageID: "dimensions"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if post.Attachments[0].Width != 80 || post.Attachments[0].Height != 60 {
		t.Fatal(post.Attachments)
	}
	if _, err := s.Copy(context.Background(), 2, id, "channel", 2, copyID); err != nil {
		t.Fatal(err)
	}
	copy, err := repo.CreateReferenced(context.Background(), 1, 2, 2, message.CreateInput{Content: "copy", AttachmentIDs: []string{copyID}, ClientMessageID: "copied-dimensions"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if copy.Attachments[0].Width != 80 || copy.Attachments[0].Height != 60 {
		t.Fatal(copy.Attachments)
	}
	if _, err := db.Exec(`UPDATE attachments SET width=NULL,height=NULL WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	page, err := repo.ListByChannelID(context.Background(), 1, 2, 2, nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range page {
		if row.ID == post.ID {
			encoded, err := json.Marshal(row.Attachments[0])
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), `"width"`) || strings.Contains(string(encoded), `"height"`) {
				t.Fatal(string(encoded))
			}
			return
		}
	}
	t.Fatal("legacy attachment message missing")
}
