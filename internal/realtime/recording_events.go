package realtime

import "voxhold-backend/internal/recordings"

func (h *Hub) HasVoiceParticipant(userID, serverID, channelID int64) bool {
	h.voice.mu.RLock()
	defer h.voice.mu.RUnlock()
	client := h.voice.byUser[userID]
	if client == nil {
		return false
	}
	participant, ok := h.voice.byClient[client]
	return ok && participant.ServerID == serverID && participant.ChannelID == channelID
}
func (h *Hub) PublishRecordingChanged(record recordings.Record) {
	h.PublishToServer(record.ServerID, OutgoingEvent{Type: EventType("recording.updated"), Data: record})
}
