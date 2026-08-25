package realtimehttp

import (
	"testing"

	"voxhold-backend/internal/realtime"
)

func TestQueueStreamStateErrorReportsRenditionFailures(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		code    realtime.ErrorCode
		message string
	}{
		{
			name:    "invalid metadata",
			err:     realtime.ErrStreamRenditionsInvalid,
			code:    realtime.ErrorInvalidPayload,
			message: "stream renditions are invalid",
		},
		{
			name:    "no fallback",
			err:     realtime.ErrStreamRenditionUnavailable,
			code:    realtime.ErrorInvalidState,
			message: "compatible stream rendition is not available",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := realtime.NewClient(1, "viewer", []int64{10})
			if err := queueStreamStateError(client, "request", test.err); err != nil {
				t.Fatal(err)
			}
			event := <-client.Outgoing()
			data, ok := event.Data.(realtime.ErrorData)
			if !ok || event.Type != realtime.EventError ||
				data.Code != test.code || data.Message != test.message {

				t.Fatalf("unexpected error event: %+v", event)
			}
		})
	}
}
