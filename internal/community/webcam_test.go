package community

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestCallCameraMetadataIsValidatedAndRelayedToCurrentPeer(t *testing.T) {
	f := setup(t)
	cid := number(t, f.call(t, 1, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{2}}, 200), "id")
	url := fmt.Sprintf("/api/v1/conversations/%d/call", cid)
	f.call(t, 1, "PUT", url, map[string]any{"session_id": "camera-owner-session"}, 200)
	f.call(t, 2, "PUT", url, map[string]any{"session_id": "camera-viewer-session"}, 200)
	var payload []byte
	f.s.notify = func(uid int64, event string, value any) {
		if uid == 2 && event == "conversation.call-signal" {
			payload, _ = json.Marshal(value)
		}
	}
	media := map[string]any{"screen": true, "revision": 1, "camera": true, "camera_stream_id": "webcam-stream"}
	input := map[string]any{"session_id": "camera-owner-session", "target_user_id": 2, "target_session_id": "camera-viewer-session", "media": media}
	f.call(t, 1, "POST", url+"/signal", input, 200)
	if !strings.Contains(string(payload), `"camera_stream_id":"webcam-stream"`) || !strings.Contains(string(payload), `"camera":true`) {
		t.Fatalf("lost camera metadata: %s", payload)
	}
	media["camera_stream_id"] = ""
	f.call(t, 1, "POST", url+"/signal", input, 400)
	media["camera_stream_id"] = strings.Repeat("x", 129)
	f.call(t, 1, "POST", url+"/signal", input, 400)
	media["camera_stream_id"] = "webcam-stream"
	input["target_session_id"] = "obsolete-viewer-session"
	f.call(t, 1, "POST", url+"/signal", input, 403)
}
