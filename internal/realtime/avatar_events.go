package realtime

// Avatar payloads are fetched separately, so room events stay small.
func (h *Hub) PublishAvatarChanged(serverIDs []int64, userID int64) {
	for _, serverID := range serverIDs {
		h.PublishToServer(serverID, OutgoingEvent{Type: EventType("avatar.updated"), Data: map[string]int64{"user_id": userID}})
	}
}
