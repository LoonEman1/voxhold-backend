package realtimehttp

import (
	"encoding/json"

	"voxhold-backend/internal/e2ee"
	"voxhold-backend/internal/realtime"
)

func (h *Handler) relayE2EEBatch(client *realtime.Client, event realtime.IncomingEvent) error {
	var batch realtime.E2EEKeyBatchData
	if err := json.Unmarshal(event.Data, &batch); err != nil || batch.ChannelID <= 0 || len(batch.Keys) == 0 || len(batch.Keys) > realtime.MaxE2EEBatchRecipients {
		return queueError(client, event.RequestID, realtime.ErrorInvalidPayload, "invalid e2ee key batch")
	}
	// Validate the whole batch before delivering any of it. The hub checks each
	// recipient's live room/device membership and supplies the sender identity.
	for _, key := range batch.Keys {
		if key.UserID <= 0 || !e2ee.ValidDeviceID(key.DeviceID) || key.Payload == "" || len(key.Payload) > e2ee.MaxRelayPayload || !base64Text(key.Payload) {
			return queueError(client, event.RequestID, realtime.ErrorInvalidPayload, "invalid e2ee key batch recipient")
		}
	}
	for _, key := range batch.Keys {
		h.hub.RelayVoiceE2EE(client, realtime.EventE2EEKey, realtime.E2EERelayData{
			ChannelID: batch.ChannelID, RecipientUserID: key.UserID,
			RecipientDeviceID: key.DeviceID, Payload: key.Payload,
		})
	}
	return nil
}
