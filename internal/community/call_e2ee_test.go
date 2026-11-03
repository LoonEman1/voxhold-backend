package community

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestRevokedDeviceLeavesConversationCallAndCannotRefreshItsLease(t *testing.T) {
	f := setup(t)
	device := strings.Repeat("a", 32)
	if _, err := f.db.Exec(`INSERT INTO e2ee_device_keys(user_id,device_id,public_key) VALUES(1,?,?)`, device, append([]byte{4}, make([]byte, 64)...)); err != nil {
		t.Fatal(err)
	}
	cid := number(t, f.call(t, 1, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{2}}, 200), "id")
	path := fmt.Sprintf("/api/v1/conversations/%d/call", cid)
	input := map[string]any{"session_id": "session-owner-0000001", "e2ee_device_id": device}
	f.call(t, 1, "PUT", path, input, 200)
	f.call(t, 2, "PUT", path, map[string]any{"session_id": "session-member-00001"}, 200)
	var notifications int
	f.s.notify = func(_ int64, event string, _ any) {
		if event == "conversation.call" {
			notifications++
		}
	}
	if _, err := f.db.Exec(`UPDATE e2ee_device_keys SET revoked_at=unixepoch() WHERE user_id=1`); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RevokeInvalidCallDevices(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	state, err := f.s.readCall(context.Background(), cid)
	if err != nil || len(state.Members) != 1 || state.Members[0].UserID != 2 {
		t.Fatalf("call=%+v err=%v", state, err)
	}
	if notifications != 2 {
		t.Fatalf("want membership updates for both users, got %d", notifications)
	}
	f.call(t, 1, "PUT", path, input, 400)
}

func TestCallMembersAdvertiseE2EEDevicesAndRelayKeys(t *testing.T) {
	f := setup(t)
	key := append([]byte{4}, make([]byte, 64)...)
	if _, err := f.db.Exec(`INSERT INTO e2ee_device_keys(user_id,device_id,public_key) VALUES(1,?,?),(2,?,?)`, strings.Repeat("a", 32), key, strings.Repeat("b", 32), key); err != nil {
		t.Fatal(err)
	}
	cid := number(t, f.call(t, 1, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{2}}, 200), "id")
	url := fmt.Sprintf("/api/v1/conversations/%d/call", cid)
	one, two := "session-owner-00000001", "session-member-000001"
	f.call(t, 1, "PUT", url, map[string]any{"session_id": one, "e2ee_device_id": strings.Repeat("c", 32)}, 400)
	f.call(t, 1, "PUT", url, map[string]any{"session_id": one, "e2ee_device_id": strings.Repeat("a", 32)}, 200)
	state := f.call(t, 2, "PUT", url, map[string]any{"session_id": two, "e2ee_device_id": strings.Repeat("b", 32)}, 200)
	var members []CallMember
	if err := json.Unmarshal(state["members"], &members); err != nil || len(members) != 2 || members[0].E2EEDeviceID != strings.Repeat("a", 32) || members[1].E2EEDeviceID != strings.Repeat("b", 32) {
		t.Fatalf("members=%+v err=%v", members, err)
	}
	var relayed any
	f.s.notify = func(uid int64, event string, data any) {
		if event == "conversation.call-signal" {
			relayed = data
		}
	}
	signal := func(e2ee map[string]any, want int) {
		t.Helper()
		f.call(t, 1, "POST", url+"/signal", map[string]any{"session_id": one, "target_user_id": 2, "target_session_id": two, "e2ee": e2ee}, want)
	}
	signal(map[string]any{"payload": "AAAA"}, 200)
	if raw, _ := json.Marshal(relayed); !strings.Contains(string(raw), `"payload":"AAAA"`) {
		t.Fatalf("relayed %s", raw)
	}
	signal(map[string]any{"request": true}, 200)
	signal(map[string]any{"payload": "AAAA", "request": true}, 400)
	signal(map[string]any{}, 400)
	signal(map[string]any{"payload": strings.Repeat("A", 1025)}, 400)
}
