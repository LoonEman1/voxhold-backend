package realtime

const (
	EventStreamStart           EventType = "stream.start"
	EventStreamStarted         EventType = "stream.started"
	EventStreamUpdated         EventType = "stream.updated"
	EventStreamStop            EventType = "stream.stop"
	EventStreamStopped         EventType = "stream.stopped"
	EventStreamWatch           EventType = "stream.watch"
	EventStreamWatching        EventType = "stream.watching"
	EventStreamLeave           EventType = "stream.leave"
	EventStreamLeft            EventType = "stream.left"
	EventStreamSnapshot        EventType = "stream.snapshot"
	EventStreamViewerJoined    EventType = "stream.viewer_joined"
	EventStreamViewerLeft      EventType = "stream.viewer_left"
	EventStreamWebRTCOffer     EventType = "stream.webrtc_offer"
	EventStreamWebRTCAnswer    EventType = "stream.webrtc_answer"
	EventStreamWebRTCAnswered  EventType = "stream.webrtc_answered"
	EventStreamICECandidate    EventType = "stream.ice_candidate"
	EventStreamWebRTCClosed    EventType = "stream.webrtc_closed"
	EventStreamP2POffer        EventType = "stream.p2p_offer"
	EventStreamP2PAnswer       EventType = "stream.p2p_answer"
	EventStreamP2PICECandidate EventType = "stream.p2p_ice_candidate"
	EventStreamP2PRestart      EventType = "stream.p2p_restart"
)

type StreamMode string
type StreamCodec string

const (
	StreamModeServer StreamMode = "server"
	StreamModeP2P    StreamMode = "p2p"

	StreamCodecVP8  StreamCodec = "vp8"
	StreamCodecVP9  StreamCodec = "vp9"
	StreamCodecH264 StreamCodec = "h264"
	StreamCodecAV1  StreamCodec = "av1"
)

type StreamStartData struct {
	ServerID  int64       `json:"server_id"`
	ChannelID int64       `json:"channel_id"`
	Mode      StreamMode  `json:"mode"`
	Codec     StreamCodec `json:"codec"`
	HasAudio  bool        `json:"has_audio"`
}

type StreamWatchData struct {
	ServerID  int64 `json:"server_id"`
	ChannelID int64 `json:"channel_id"`
}

type StreamData struct {
	ServerID              int64       `json:"server_id"`
	ChannelID             int64       `json:"channel_id"`
	PublisherUserID       int64       `json:"publisher_user_id"`
	PublisherConnectionID string      `json:"publisher_connection_id"`
	Mode                  StreamMode  `json:"mode"`
	Codec                 StreamCodec `json:"codec"`
	HasAudio              bool        `json:"has_audio"`
	ViewerCount           int         `json:"viewer_count"`
}

type StreamWatchingData struct {
	Stream             StreamData `json:"stream"`
	ViewerConnectionID string     `json:"viewer_connection_id"`
}

type StreamStoppedData struct {
	ServerID  int64  `json:"server_id"`
	ChannelID int64  `json:"channel_id"`
	Reason    string `json:"reason,omitempty"`
}

type StreamViewerData struct {
	ConnectionID string `json:"connection_id"`
	UserID       int64  `json:"user_id"`
}

type StreamSnapshotData struct {
	Streams []StreamData `json:"streams"`
}

type StreamWebRTCOfferData struct {
	SDP string `json:"sdp"`
}

type StreamWebRTCAnswerData struct {
	SDP string `json:"sdp"`
}

type StreamWebRTCAnsweredData struct {
	Accepted bool `json:"accepted"`
}

type StreamICECandidateData struct {
	Candidate        string  `json:"candidate"`
	SDPMid           *string `json:"sdp_mid,omitempty"`
	SDPMLineIndex    *uint16 `json:"sdp_mline_index,omitempty"`
	UsernameFragment *string `json:"username_fragment,omitempty"`
}

type StreamWebRTCClosedData struct {
	Reason string `json:"reason"`
}

type StreamP2PSessionData struct {
	TargetConnectionID string `json:"target_connection_id"`
	FromConnectionID   string `json:"from_connection_id,omitempty"`
	SDP                string `json:"sdp"`
}

type StreamP2PICECandidateData struct {
	TargetConnectionID string  `json:"target_connection_id"`
	FromConnectionID   string  `json:"from_connection_id,omitempty"`
	Candidate          string  `json:"candidate"`
	SDPMid             *string `json:"sdp_mid,omitempty"`
	SDPMLineIndex      *uint16 `json:"sdp_mline_index,omitempty"`
	UsernameFragment   *string `json:"username_fragment,omitempty"`
}

type StreamP2PRestartData struct {
	TargetConnectionID string `json:"target_connection_id"`
}
