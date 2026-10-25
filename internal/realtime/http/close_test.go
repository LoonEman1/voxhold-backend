package realtimehttp

import (
	"testing"

	"github.com/coder/websocket"

	"voxhold-backend/internal/realtime"
)

func TestCloseStatusSeparatesRestartFromPolicyViolations(t *testing.T) {
	if status := closeStatusFor(realtime.CloseReasonServerShutdown); status != websocket.StatusServiceRestart {
		t.Fatalf("server restart must close with 1012, got %d", status)
	}
	for _, reason := range []string{"session expired or revoked", "event rate limit exceeded"} {
		if status := closeStatusFor(reason); status != websocket.StatusPolicyViolation {
			t.Fatalf("%q must stay a policy violation, got %d", reason, status)
		}
	}
}
