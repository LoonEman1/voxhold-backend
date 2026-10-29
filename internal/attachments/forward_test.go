package attachments

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
	"voxhold-backend/internal/account"
	"voxhold-backend/internal/community"
	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/recordings"
	"voxhold-backend/internal/testdb"
)

type noMessageEvents struct{}

func (noMessageEvents) PublishMessageCreated(message.Message)      {}
func (noMessageEvents) PublishMessageUpdated(message.Message)      {}
func (noMessageEvents) PublishMessageDeleted(message.Message)      {}
func (noMessageEvents) PublishMessagePinned(message.PinnedMessage) {}
func (noMessageEvents) PublishMessageUnpinned(int64, int64)        {}
func TestForwardCopiesFilesAndSchedulesRetainStagedFiles(t *testing.T) {
	db := testdb.Open(t)
	t.Setenv("ATTACHMENT_DIRECTORY", t.TempDir())
	rec, err := recordings.New(db, recordings.Config{Directory: t.TempDir()}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()
	files, err := New(db, rec, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	chats := community.New(db, nil, nil)
	repo := messagesqlite.NewRepository(db)
	chats.SetMessages(message.NewService(repo, noMessageEvents{}), files.Copy)
	mux := http.NewServeMux()
	auth := func(h http.Handler) http.Handler { return h }
	files.RegisterRoutes(mux, auth)
	chats.RegisterRoutes(mux, auth)
	request := func(uid int64, method, path string, body []byte, code int) []byte {
		t.Helper()
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r = r.WithContext(account.ContextWithUserID(r.Context(), uid))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != code {
			t.Fatalf("%s %s: %d expected %d: %s", method, path, w.Code, code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	call := func(uid int64, method, path string, v any, code int) []byte {
		t.Helper()
		body, _ := json.Marshal(v)
		return request(uid, method, path, body, code)
	}
	originalID := "0123456789abcdef0123456789abcdef"
	request(2, "PUT", "/api/v1/channels/2/attachments/"+originalID+"?filename=readme.txt", []byte("independent file"), 200)
	source, err := repo.CreateReferenced(context.Background(), 1, 2, 2, message.CreateInput{Content: "file", AttachmentIDs: []string{originalID}, ClientMessageID: "file-original"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var conversation struct{ ID int64 }
	json.Unmarshal(call(1, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{2}}, 200), &conversation)
	payload := map[string]any{"source": map[string]any{"kind": "channel", "id": source.ID}, "destination": map[string]any{"kind": "conversation", "id": conversation.ID}, "client_message_id": "copy-file"}
	var forwarded community.DirectMessage
	json.Unmarshal(call(1, "POST", "/api/v1/me/forward", payload, 200), &forwarded)
	if len(forwarded.Attachments) != 1 || forwarded.Attachments[0].ID == originalID {
		t.Fatal(forwarded)
	}
	copyID := forwarded.Attachments[0].ID
	call(1, "POST", "/api/v1/me/forward", payload, 200)
	var count int
	db.QueryRow(`SELECT count(*) FROM attachments`).Scan(&count)
	if count != 2 {
		t.Fatal("retry duplicated copied files", count)
	}
	if _, err = repo.Delete(context.Background(), 1, 2, source.ID, 2); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err = os.Chtimes(filepath.Join(files.directory, originalID), old, old); err != nil {
		t.Fatal(err)
	}
	if err = files.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := request(2, "GET", "/api/v1/attachments/"+copyID+"/content", nil, 200); string(got) != "independent file" {
		t.Fatal(string(got))
	}
	request(3, "GET", "/api/v1/attachments/"+copyID+"/content", nil, 404)
	pendingID := "1123456789abcdef0123456789abcdef"
	request(2, "PUT", "/api/v1/channels/2/attachments/"+pendingID+"?filename=later.txt", []byte("scheduled file"), 200)
	data := call(2, "POST", "/api/v1/me/scheduled-messages", map[string]any{"destination": map[string]any{"kind": "channel", "id": 2}, "payload": map[string]any{"content": "later", "attachment_ids": []string{pendingID}}, "send_at": time.Now().Add(72 * time.Hour).Unix()}, 200)
	var schedule struct{ ID int64 }
	json.Unmarshal(data, &schedule)
	if err = os.Chtimes(filepath.Join(files.directory, pendingID), old, old); err != nil {
		t.Fatal(err)
	}
	if err = files.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	request(2, "GET", "/api/v1/attachments/"+pendingID+"/content", nil, 200)
	call(2, "DELETE", fmt.Sprintf("/api/v1/me/scheduled-messages/%d", schedule.ID), nil, 200)
	if err = files.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	request(2, "GET", "/api/v1/attachments/"+pendingID+"/content", nil, 410)
}
