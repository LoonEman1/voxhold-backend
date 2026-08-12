package stream

import "testing"

func TestCandidateMatchesCurrentICEGeneration(t *testing.T) {
	current := "current"
	stale := "stale"
	sdp := "v=0\r\na=ice-ufrag:current\r\n"
	if !candidateMatchesRemoteDescription(sdp, &current) {
		t.Fatal("current ICE candidate was rejected")
	}
	if candidateMatchesRemoteDescription(sdp, &stale) {
		t.Fatal("stale ICE candidate was accepted")
	}
	if !candidateMatchesRemoteDescription(sdp, nil) {
		t.Fatal("candidate without generation was rejected")
	}
}
