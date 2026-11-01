package realtimehttp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"voxhold-backend/internal/realtime"
)

type encryptionPolicy struct {
	required map[int64]bool
	devices  map[string]bool
}

func (p encryptionPolicy) ChannelRequired(_ context.Context, channelID int64) (bool, error) {
	return p.required[channelID], nil
}
func (p encryptionPolicy) DeviceActive(_ context.Context, _ int64, deviceID string) (bool, error) {
	return p.devices[deviceID], nil
}

func lastError(client *realtime.Client) string {
	message := ""
	for {
		event, ok := client.PopOutgoing()
		if !ok {
			return message
		}
		if data, isError := event.Data.(realtime.ErrorData); isError {
			message = data.Message
		}
	}
}

func TestVoiceJoinFollowsTheChannelEncryptionPolicy(t *testing.T) {
	const device = "0123456789abcdef0123456789abcdef"
	h := &Handler{encryption: encryptionPolicy{required: map[int64]bool{1: true}, devices: map[string]bool{device: true}}}
	client := realtime.NewClient(5, "client", []int64{1})
	cases := []struct {
		name         string
		data         realtime.VoiceJoinData
		ok, required bool
		message      string
	}{
		{"encrypted channel without a device", realtime.VoiceJoinData{ChannelID: 1}, false, true, "requires end-to-end encryption"},
		{"encrypted channel with an unknown device", realtime.VoiceJoinData{ChannelID: 1, E2EEDeviceID: strings.Repeat("f", 32)}, false, true, "not registered"},
		{"encrypted channel with a registered device", realtime.VoiceJoinData{ChannelID: 1, E2EEDeviceID: device}, true, true, ""},
		{"plain channel with a device", realtime.VoiceJoinData{ChannelID: 2, E2EEDeviceID: device}, false, false, "does not use"},
		{"plain channel", realtime.VoiceJoinData{ChannelID: 2}, true, false, ""},
	}
	for _, c := range cases {
		required, ok, err := h.checkVoiceEncryption(context.Background(), client, "r", c.data)
		message := lastError(client)
		if err != nil || ok != c.ok || required != c.required || !strings.Contains(message, c.message) || c.message == "" && message != "" {
			t.Errorf("%s: ok=%v required=%v err=%v message=%q", c.name, ok, required, err, message)
		}
	}
}

func TestE2EERelayValidatesThePayload(t *testing.T) {
	h := &Handler{hub: realtime.NewHub()}
	client := realtime.NewClient(5, "client", []int64{1})
	relay := func(eventType realtime.EventType, data map[string]any) string {
		raw, _ := json.Marshal(data)
		if err := h.relayE2EE(client, realtime.IncomingEvent{Type: eventType, Data: raw}); err != nil {
			t.Fatal(err)
		}
		return lastError(client)
	}
	valid := map[string]any{"channel_id": 1, "recipient_user_id": 2, "recipient_device_id": strings.Repeat("a", 32), "payload": "AAAA"}
	if message := relay(realtime.EventE2EEKey, valid); message != "" {
		t.Fatalf("valid relay rejected: %s", message)
	}
	oversized := map[string]any{"channel_id": 1, "recipient_user_id": 2, "recipient_device_id": strings.Repeat("a", 32), "payload": strings.Repeat("A", 1025)}
	notBase64 := map[string]any{"channel_id": 1, "recipient_user_id": 2, "recipient_device_id": strings.Repeat("a", 32), "payload": "{json}"}
	badDevice := map[string]any{"channel_id": 1, "recipient_user_id": 2, "recipient_device_id": "x", "payload": "AAAA"}
	for name, data := range map[string]map[string]any{"oversized": oversized, "not base64": notBase64, "bad device": badDevice} {
		if message := relay(realtime.EventE2EEKey, data); message == "" {
			t.Errorf("%s accepted", name)
		}
	}
	request := map[string]any{"channel_id": 1, "recipient_user_id": 2, "recipient_device_id": strings.Repeat("a", 32)}
	if message := relay(realtime.EventE2EEKeyRequest, request); message != "" {
		t.Fatalf("key request rejected: %s", message)
	}
}
