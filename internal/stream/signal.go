package stream

import "errors"

const MaxPendingICECandidates = 64

type Codec string

const (
	CodecVP8  Codec = "vp8"
	CodecVP9  Codec = "vp9"
	CodecH264 Codec = "h264"
	CodecAV1  Codec = "av1"
)

var (
	ErrSessionNotFound      = errors.New("stream WebRTC session not found")
	ErrStreamExists         = errors.New("channel already has an active stream")
	ErrStreamNotFound       = errors.New("channel has no active stream")
	ErrViewerLimit          = errors.New("stream viewer limit reached")
	ErrTooManyICECandidates = errors.New("too many pending stream ICE candidates")
)

type ICECandidate struct {
	Candidate        string  `json:"candidate"`
	SDPMid           *string `json:"sdp_mid,omitempty"`
	SDPMLineIndex    *uint16 `json:"sdp_mline_index,omitempty"`
	UsernameFragment *string `json:"username_fragment,omitempty"`
}

type SignalSink interface {
	SendOffer(connectionID string, sdp string) bool
	SendICECandidate(connectionID string, candidate ICECandidate) bool
	CloseStream(connectionID string, reason string)
}
