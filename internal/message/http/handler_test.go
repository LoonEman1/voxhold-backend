package messagehttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"voxhold-backend/internal/account"
	"voxhold-backend/internal/message"
)

type pinServiceStub struct {
	Service
	value message.PinnedMessage
}

func (s pinServiceStub) Pin(
	_ context.Context,
	serverID int64,
	channelID int64,
	messageID int64,
	userID int64,
) (message.PinnedMessage, error) {
	if serverID != 2 || channelID != 9 || messageID != 41 || userID != 7 {
		return message.PinnedMessage{}, message.ErrMessageNotFound
	}

	return s.value, nil
}

func TestPinReturnsPinnedMessage(t *testing.T) {
	value := message.PinnedMessage{
		Message: message.Message{
			ID:        41,
			ChannelID: 9,
			Author: message.Author{
				UserID:   8,
				Username: "author",
			},
			Content:   "important",
			CreatedAt: 100,
		},
		PinnedBy: message.Author{
			UserID:   7,
			Username: "owner",
		},
		PinnedAt: 200,
	}

	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/servers/2/channels/9/messages/41/pin",
		nil,
	)
	request.SetPathValue("serverID", "2")
	request.SetPathValue("channelID", "9")
	request.SetPathValue("messageID", "41")
	request = request.WithContext(
		account.ContextWithUserID(request.Context(), 7),
	)
	response := httptest.NewRecorder()

	NewHandler(pinServiceStub{value: value}).pin(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d; body = %s",
			response.Code,
			http.StatusOK,
			response.Body.String(),
		)
	}

	var payload pinnedMessageResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Message.ID != 41 ||
		payload.Message.Content != "important" ||
		payload.PinnedBy.UserID != 7 ||
		payload.PinnedAt != 200 {

		t.Fatalf("unexpected response: %#v", payload)
	}
}
