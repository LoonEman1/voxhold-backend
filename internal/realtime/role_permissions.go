package realtime

// SetRolePermissionLoader is configured before accepting connections.
func (h *Hub) SetRolePermissionLoader(loader func(int64, int64) (int, error)) {
	h.rolePermissions = loader
}

// Permission revocation also applies to media already running.
func (h *Hub) RefreshRolePermissions(serverID int64) {
	h.channelAccessMu.Lock()
	clients := h.clientsForServer(serverID)
	for _, client := range clients {
		unlock := h.mediaTransitions.Lock(client.UserID())
		if h.rolePermissions != nil {
			bits, err := h.rolePermissions(serverID, client.UserID())
			if err != nil || bits&4 == 0 {
				h.leaveVoiceForServer(client, serverID)
				h.SendToConnection(client.ConnectionID(), OutgoingEvent{Type: EventVoiceWebRTCClosed, Data: VoiceWebRTCClosedData{Reason: "voice permission was revoked"}})
			} else if bits&8 == 0 {
				if current, ok := h.voice.current(client); ok && current.ServerID == serverID && h.streams.publisherForChannel(current.ChannelID) == client {
					h.leaveStream(client, "stream permission was revoked")
				}
			}
		}
		unlock()
	}
	h.channelAccessMu.Unlock()
	h.RefreshChannelAccess(serverID)
}
