package realtimehttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"voxhold-backend/internal/antiabuse"
	"voxhold-backend/internal/realtime"
)

func TestFullRoomKeysAndACKsFitDefaultWebSocketBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hub := realtime.NewHub()
	defer hub.Close()
	clients := make([]*realtime.Client, 32)
	keys := make([]realtime.E2EEKeyRecipient, 0, 31)
	for i := range clients {
		device := fmt.Sprintf("%032x", i+1)
		clients[i] = realtime.NewClient(int64(i+1), fmt.Sprint(i), []int64{1})
		hub.Register(clients[i])
		hub.JoinVoiceWithDevice(clients[i], 1, 1, false, false, device)
		if i > 0 {
			keys = append(keys, realtime.E2EEKeyRecipient{UserID: int64(i + 1), DeviceID: device, Payload: "AQID"})
		}
	}
	for _, client := range clients {
		for {
			if _, ok := client.PopOutgoing(); !ok {
				break
			}
		}
	}
	h := &Handler{hub: hub, protector: antiabuse.New(antiabuse.DefaultConfig())}
	finished := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			finished <- err
			return
		}
		defer conn.CloseNow()
		finished <- h.readLoop(ctx, conn, clients[0])
	}))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { conn.CloseNow(); cancel(); <-finished }()
	for range 2 { // 31 sender keys, then 31 pairwise readiness ACKs.
		batch := realtime.E2EEKeyBatchData{ChannelID: 1, Keys: keys}
		if err := wsjson.Write(ctx, conn, realtime.OutgoingEvent{Type: realtime.EventE2EEKeys, Data: batch}); err != nil {
			t.Fatal(err)
		}
	}
	if err := wsjson.Write(ctx, conn, realtime.OutgoingEvent{RequestID: "after-burst", Type: realtime.EventPing}); err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("WebSocket stopped processing after key/ACK burst")
			return
		case <-ticker.C:
			if event, ok := clients[0].PopOutgoing(); ok && event.Type == realtime.EventPong {
				for _, client := range clients[1:] {
					for range 2 {
						event, ok := client.PopOutgoing()
						if !ok || event.Type != realtime.EventE2EEKey {
							t.Fatal("room member did not receive both key and ACK")
						}
					}
				}
				return
			}
		}
	}
}

func TestE2EEBatchValidatesBeforeRelayingAndKeepsRoomIsolation(t *testing.T) {
	hub := realtime.NewHub()
	defer hub.Close()
	deviceA, deviceB := strings.Repeat("a", 32), strings.Repeat("b", 32)
	alice := realtime.NewClient(1, "alice", []int64{10})
	bob := realtime.NewClient(2, "bob", []int64{10})
	carol := realtime.NewClient(3, "carol", []int64{10})
	for _, client := range []*realtime.Client{alice, bob, carol} {
		hub.Register(client)
	}
	hub.JoinVoiceWithDevice(alice, 10, 100, false, false, deviceA)
	hub.JoinVoiceWithDevice(bob, 10, 100, false, false, deviceB)
	hub.JoinVoiceWithDevice(carol, 10, 101, false, false, deviceB)
	drain := func(client *realtime.Client) []realtime.OutgoingEvent {
		var keys []realtime.OutgoingEvent
		for {
			event, ok := client.PopOutgoing()
			if !ok {
				return keys
			}
			if event.Type == realtime.EventE2EEKey {
				keys = append(keys, event)
			}
		}
	}
	for _, client := range []*realtime.Client{alice, bob, carol} {
		drain(client)
	}
	h := &Handler{hub: hub}
	relay := func(batch realtime.E2EEKeyBatchData) {
		t.Helper()
		raw, _ := json.Marshal(batch)
		if err := h.relayE2EEBatch(alice, realtime.IncomingEvent{Type: realtime.EventE2EEKeys, Data: raw}); err != nil {
			t.Fatal(err)
		}
	}
	valid := realtime.E2EEKeyRecipient{UserID: 2, DeviceID: deviceB, Payload: "AQID"}
	relay(realtime.E2EEKeyBatchData{ChannelID: 100, Keys: []realtime.E2EEKeyRecipient{valid, {UserID: 3, DeviceID: deviceB, Payload: "AQID"}}})
	keys := drain(bob)
	if len(keys) != 1 || len(drain(carol)) != 0 {
		t.Fatalf("keys escaped room isolation: bob=%d", len(keys))
	}
	data := keys[0].Data.(realtime.E2EERelayData)
	if data.SenderUserID != 1 || data.SenderDeviceID != deviceA || data.SenderConnectionID != alice.ConnectionID() {
		t.Fatalf("sender identity not supplied by hub: %+v", data)
	}
	for name, batch := range map[string]realtime.E2EEKeyBatchData{
		"empty":                    {ChannelID: 100},
		"oversized":                {ChannelID: 100, Keys: make([]realtime.E2EEKeyRecipient, 33)},
		"invalid second recipient": {ChannelID: 100, Keys: []realtime.E2EEKeyRecipient{valid, {UserID: 2, DeviceID: "invalid", Payload: "AQID"}}},
		"oversized key":            {ChannelID: 100, Keys: []realtime.E2EEKeyRecipient{{UserID: 2, DeviceID: deviceB, Payload: strings.Repeat("A", 1025)}}},
	} {
		t.Run(name, func(t *testing.T) {
			relay(batch)
			if message := lastError(alice); message == "" {
				t.Fatal("invalid batch accepted")
			}
			if len(drain(bob)) != 0 {
				t.Fatal("part of an invalid batch was delivered")
			}
		})
	}
	hub.LeaveVoice(bob)
	drain(bob)
	relay(realtime.E2EEKeyBatchData{ChannelID: 100, Keys: []realtime.E2EEKeyRecipient{valid}})
	if len(drain(bob)) != 0 {
		t.Fatal("key delivered after recipient left")
	}
}
