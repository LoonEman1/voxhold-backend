package realtime

const (
	EventVoiceJoin              EventType = "voice.join"
	EventVoiceJoined            EventType = "voice.joined"
	EventVoiceLeave             EventType = "voice.leave"
	EventVoiceLeft              EventType = "voice.left"
	EventVoiceStateUpdate       EventType = "voice.state_update"
	EventVoiceStateUpdated      EventType = "voice.state_updated"
	EventVoiceParticipantJoined EventType = "voice.participant_joined"
	EventVoiceParticipantLeft   EventType = "voice.participant_left"
	EventVoiceSnapshot          EventType = "voice.snapshot"
)

type VoiceJoinData struct {
	ServerID  int64 `json:"server_id"`
	ChannelID int64 `json:"channel_id"`
	SelfMute  bool  `json:"self_mute"`
	SelfDeaf  bool  `json:"self_deaf"`
}

type VoiceStateUpdateData struct {
	SelfMute bool `json:"self_mute"`
	SelfDeaf bool `json:"self_deaf"`
}

type VoiceParticipantData struct {
	ConnectionID string `json:"connection_id"`
	UserID       int64  `json:"user_id"`
	ServerID     int64  `json:"server_id"`
	ChannelID    int64  `json:"channel_id"`
	SelfMute     bool   `json:"self_mute"`
	SelfDeaf     bool   `json:"self_deaf"`
}

type VoiceJoinedData struct {
	Participant  VoiceParticipantData   `json:"participant"`
	Participants []VoiceParticipantData `json:"participants"`
}

type VoiceLeftData struct {
	ConnectionID string `json:"connection_id"`
	UserID       int64  `json:"user_id"`
	ServerID     int64  `json:"server_id"`
	ChannelID    int64  `json:"channel_id"`
}

type VoiceSnapshotData struct {
	Participants []VoiceParticipantData `json:"participants"`
}
