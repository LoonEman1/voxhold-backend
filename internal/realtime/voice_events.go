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
	EventVoiceWebRTCOffer       EventType = "voice.webrtc_offer"
	EventVoiceWebRTCAnswer      EventType = "voice.webrtc_answer"
	EventVoiceWebRTCAnswered    EventType = "voice.webrtc_answered"
	EventVoiceICECandidate      EventType = "voice.ice_candidate"
	EventVoiceWebRTCClosed      EventType = "voice.webrtc_closed"
	EventVoiceModerate          EventType = "voice.moderate"
	EventVoiceModerated         EventType = "voice.moderated"

	// FEAT-13: opaque wrapped sender keys relayed between room members.
	EventE2EEKey        EventType = "e2ee.key"
	EventE2EEKeyRequest EventType = "e2ee.key_request"
)

const (
	VoiceWebRTCClosedReasonReplaced = "voice session moved to another connection"
	// Sent to everyone in a channel whose encryption policy changed: they
	// rejoin in the new mode instead of mixing encrypted and plain audio.
	VoiceWebRTCClosedReasonE2EEPolicy = "voice channel encryption changed"
	VoiceWebRTCClosedReasonE2EEDevice = "voice e2ee device was revoked"
)

type VoiceJoinData struct {
	ServerID           int64  `json:"server_id"`
	ChannelID          int64  `json:"channel_id"`
	SelfMute           bool   `json:"self_mute"`
	SelfDeaf           bool   `json:"self_deaf"`
	SupportsMove       bool   `json:"supports_move,omitempty"`
	ResumeConnectionID string `json:"resume_connection_id,omitempty"`
	// E2EEDeviceID is set only by clients joining an encrypted channel.
	E2EEDeviceID string `json:"e2ee_device_id,omitempty"`
}

type VoiceStateUpdateData struct {
	SelfMute  bool  `json:"self_mute"`
	SelfDeaf  bool  `json:"self_deaf"`
	SelfVideo *bool `json:"self_video,omitempty"`
}

type VoiceModerationData struct {
	ServerID             int64  `json:"server_id"`
	ChannelID            int64  `json:"channel_id"`
	UserID               int64  `json:"user_id"`
	ConnectionID         string `json:"connection_id"`
	Action               string `json:"action"`
	Muted                *bool  `json:"muted,omitempty"`
	DestinationChannelID int64  `json:"destination_channel_id,omitempty"`
}

type VoiceParticipantData struct {
	generation   uint64
	ConnectionID string `json:"connection_id"`
	UserID       int64  `json:"user_id"`
	ServerID     int64  `json:"server_id"`
	ChannelID    int64  `json:"channel_id"`
	SelfMute     bool   `json:"self_mute"`
	SelfDeaf     bool   `json:"self_deaf"`
	SelfVideo    bool   `json:"self_video,omitempty"`
	ServerMute   bool   `json:"server_mute,omitempty"`
	SupportsMove bool   `json:"supports_move,omitempty"`
	Reconnecting bool   `json:"reconnecting,omitempty"`
	// E2EEDeviceID tells the other members whose device key wraps this
	// participant's sender key.
	E2EEDeviceID string `json:"e2ee_device_id,omitempty"`
}

type VoiceJoinedData struct {
	Participant  VoiceParticipantData   `json:"participant"`
	Participants []VoiceParticipantData `json:"participants"`
	E2EERequired bool                   `json:"e2ee_required"`
}

// E2EERelayData is both the client request and the relayed event. The server
// fills the sender fields and never reads Payload.
type E2EERelayData struct {
	ChannelID          int64  `json:"channel_id"`
	RecipientUserID    int64  `json:"recipient_user_id,omitempty"`
	RecipientDeviceID  string `json:"recipient_device_id,omitempty"`
	SenderUserID       int64  `json:"sender_user_id,omitempty"`
	SenderDeviceID     string `json:"sender_device_id,omitempty"`
	SenderConnectionID string `json:"sender_connection_id,omitempty"`
	Payload            string `json:"payload,omitempty"`
}

// EventChannelID applies the channel access filter to relayed keys.
func (d E2EERelayData) EventChannelID() int64 { return d.ChannelID }

type VoiceLeftData struct {
	ConnectionID         string `json:"connection_id"`
	UserID               int64  `json:"user_id"`
	ServerID             int64  `json:"server_id"`
	ChannelID            int64  `json:"channel_id"`
	Reason               string `json:"reason,omitempty"`
	DestinationChannelID int64  `json:"destination_channel_id,omitempty"`
}

type VoiceSnapshotData struct {
	Participants []VoiceParticipantData `json:"participants"`
}

type VoiceWebRTCOfferData struct {
	SDP string `json:"sdp"`
}

type VoiceWebRTCAnswerData struct {
	SDP string `json:"sdp"`
}

type VoiceWebRTCAnsweredData struct {
	Accepted bool `json:"accepted"`
}

type VoiceICECandidateData struct {
	Candidate        string  `json:"candidate"`
	SDPMid           *string `json:"sdp_mid,omitempty"`
	SDPMLineIndex    *uint16 `json:"sdp_mline_index,omitempty"`
	UsernameFragment *string `json:"username_fragment,omitempty"`
}

type VoiceWebRTCClosedData struct {
	Reason string `json:"reason"`
}
