package voice

import (
	"errors"
	"testing"

	"github.com/pion/webrtc/v4"
)

func TestSessionLimitsPendingICECandidates(t *testing.T) {
	peer, err := webrtc.NewPeerConnection(
		webrtc.Configuration{},
	)
	if err != nil {
		t.Fatalf("create peer connection: %v", err)
	}
	defer peer.Close()

	value := &session{peer: peer}
	candidate := ICECandidate{Candidate: "candidate:test"}

	for index := 0; index < MaxPendingICECandidates; index++ {
		if err := value.addICECandidate(candidate); err != nil {
			t.Fatalf("candidate %d was rejected: %v", index, err)
		}
	}

	err = value.addICECandidate(candidate)
	if !errors.Is(err, ErrTooManyICECandidates) {
		t.Fatalf("unexpected candidate limit error: %v", err)
	}
	if len(value.pendingCandidates) != MaxPendingICECandidates {
		t.Fatalf(
			"pending candidate queue grew past its limit: %d",
			len(value.pendingCandidates),
		)
	}
}
