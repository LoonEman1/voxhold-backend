package voice

import "errors"

const MaxPendingICECandidates = 64

var ErrTooManyICECandidates = errors.New(
	"too many pending ICE candidates",
)

type ICECandidate struct {
	Candidate        string  `json:"candidate"`
	SDPMid           *string `json:"sdp_mid,omitempty"`
	SDPMLineIndex    *uint16 `json:"sdp_mline_index,omitempty"`
	UsernameFragment *string `json:"username_fragment,omitempty"`
}

type SignalSink interface {
	SendOffer(connectionID string, sdp string) bool

	SendICECandidate(
		connectionID string,
		candidate ICECandidate,
	) bool

	CloseVoice(connectionID string, reason string)
}
