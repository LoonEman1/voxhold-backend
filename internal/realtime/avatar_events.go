package realtime

type AvatarChangedData struct {
	UserID int64 `json:"user_id"`
	// Version is the new ETag, or null after removal; clients fetch only known versions.
	Version *string `json:"version"`
}

// Avatar payloads are fetched separately, so room events stay small.
func (h *Hub) PublishAvatarChanged(serverIDs []int64, userID int64, version *string) {
	for _, serverID := range serverIDs {
		h.PublishToServer(serverID, OutgoingEvent{Type: EventType("avatar.updated"), Data: AvatarChangedData{UserID: userID, Version: version}})
	}
}
