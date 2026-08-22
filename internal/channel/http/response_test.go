package channelhttp

import (
	"encoding/json"
	"testing"

	"voxhold-backend/internal/channel"
)

func TestChannelsResponseIncludesLastMessageID(t *testing.T) {
	response := newChannelsResponse([]channel.Channel{
		{
			ID:            10,
			LastMessageID: 105,
		},
	})

	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var decoded []map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got := decoded[0]["last_message_id"]; got != float64(105) {
		t.Fatalf("last_message_id = %v, want 105", got)
	}
}
