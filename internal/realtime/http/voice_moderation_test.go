package realtimehttp

import (
	"context"
	"encoding/json"
	"testing"

	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"
	"voxhold-backend/internal/permissions"
	"voxhold-backend/internal/realtime"
	"voxhold-backend/internal/testdb"
)

type moderationMedia struct {
	VoiceMedia
	muted bool
	left  string
	full  bool
}

func (m *moderationMedia) RoomFull(int64) bool { return m.full }

func (m *moderationMedia) Join(_ string, _, _, _ int64, mute, _ bool) error {
	m.muted = mute
	return nil
}

func (m *moderationMedia) SetState(_ string, mute, _ bool) error {
	m.muted = mute
	return nil
}

func (m *moderationMedia) Leave(connectionID string) { m.left = connectionID }

func TestVoiceModerationPermissionsAndSessionLifecycle(t *testing.T) {
	db := testdb.Open(t)
	_, err := db.Exec(`INSERT INTO server_members(server_id,user_id,role) VALUES(1,3,'member');
		INSERT INTO server_roles(id,server_id,name,permissions,voice_permissions) VALUES(1,1,'voice moderator',1,32);
		INSERT INTO member_roles(server_id,user_id,role_id) VALUES(1,3,1);
		UPDATE channels SET access_restricted=1 WHERE id=4;`)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	hub := realtime.NewHub()
	media := &moderationMedia{}
	hub.SetVoiceSessionCloser(media)
	h := &Handler{hub: hub, voiceMedia: media, channelAccess: channel.NewService(channelsqlite.NewRepository(db), nil)}
	h.SetVoiceModeration(permissions.NewVoiceModeration(db))
	owner := realtime.NewClient(1, "owner", []int64{1})
	member := realtime.NewClient(2, "member", []int64{1})
	moderator := realtime.NewClient(3, "moderator", []int64{1})
	for _, client := range []*realtime.Client{owner, member, moderator} {
		if !hub.Register(client) {
			t.Fatal("failed to register client")
		}
		defer hub.Unregister(client)
	}
	input := func(data any) realtime.IncomingEvent {
		t.Helper()
		raw, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		return realtime.IncomingEvent{RequestID: "request", Data: raw}
	}
	join := func(channelID int64) {
		t.Helper()
		if err := h.joinVoice(ctx, member, input(realtime.VoiceJoinData{ServerID: 1, ChannelID: channelID, SupportsMove: true})); err != nil {
			t.Fatal(err)
		}
		if message := lastError(member); message != "" {
			t.Fatal(message)
		}
	}
	action := func(actor *realtime.Client, action string, muted *bool, destination int64) string {
		t.Helper()
		err := h.moderateVoice(ctx, actor, input(realtime.VoiceModerationData{
			ServerID: 1, ChannelID: 1, UserID: 2, ConnectionID: member.ConnectionID(), Action: action,
			Muted: muted, DestinationChannelID: destination,
		}))
		if err != nil {
			t.Fatal(err)
		}
		return lastError(actor)
	}
	expectLeft := func(reason string, destination int64) {
		t.Helper()
		for event, ok := member.PopOutgoing(); ok; event, ok = member.PopOutgoing() {
			if event.Type == realtime.EventVoiceLeft {
				left := event.Data.(realtime.VoiceLeftData)
				if left.Reason != reason || left.DestinationChannelID != destination || media.left != member.ConnectionID() {
					t.Fatalf("unexpected voice departure: %+v, closed=%q", left, media.left)
				}
				return
			}
		}
		t.Fatal("missing voice.left event")
	}
	join(1)
	if message := action(moderator, "kick", nil, 0); message != "voice moderation permission is required" {
		t.Fatalf("moderator gained kick permission: %q", message)
	}
	if message := action(owner, "move", nil, 4); message != "voice channel is unavailable" {
		t.Fatalf("restricted destination accepted: %q", message)
	}
	muted := true
	if message := action(moderator, "mute", &muted, 0); message != "" || !media.muted {
		t.Fatalf("custom-role mute did not apply: %q", message)
	}
	if err := h.updateVoiceState(member, input(realtime.VoiceStateUpdateData{})); err != nil || !media.muted {
		t.Fatalf("self-unmute overrode the server mute: %v", err)
	}
	if _, err := db.Exec(`UPDATE channels SET access_restricted=0 WHERE id=4`); err != nil {
		t.Fatal(err)
	}
	media.full = true
	if message := action(owner, "move", nil, 4); message != "voice channel is full" || media.left != "" {
		t.Fatalf("moving into a full channel disturbed the source session: %q", message)
	}
	media.full = false
	if message := action(owner, "move", nil, 4); message != "" {
		t.Fatal(message)
	}
	expectLeft("moved", 4)
	join(4)
	participant, ok := hub.CurrentVoice(member)
	if !ok || !participant.ServerMute || !media.muted {
		t.Fatal("move lost the server mute")
	}
	join(1)
	muted = false
	if message := action(owner, "mute", &muted, 0); message != "" || media.muted {
		t.Fatalf("unmute did not restore audio: %q", message)
	}
	if message := action(owner, "kick", nil, 0); message != "" {
		t.Fatal(message)
	}
	expectLeft("kicked", 0)
	join(1)
}
