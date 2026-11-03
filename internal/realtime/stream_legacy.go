package realtime

// Called with queueMu held, after channel authorization. Older clients keep
// their single primary per channel and never receive another publisher's stop.
func (c *Client) filterLegacyStreamEvent(event OutgoingEvent) (OutgoingEvent, bool) {
	if c.SupportsMultipleStreams() {
		return event, true
	}
	switch data := event.Data.(type) {
	case StreamSnapshotData:
		primary := make(map[int64]string)
		streams := make([]StreamData, 0, len(data.Streams))
		for _, value := range data.Streams {
			if _, exists := primary[value.ChannelID]; !exists {
				primary[value.ChannelID] = value.PublisherConnectionID
				streams = append(streams, value)
			}
		}
		c.legacyStreams = primary
		data.Streams = streams
		event.Data = data
		event.encoding = nil
	case StreamData:
		if c.legacyStreams == nil {
			c.legacyStreams = make(map[int64]string)
		}
		if id, exists := c.legacyStreams[data.ChannelID]; exists && id != data.PublisherConnectionID && event.RequestID == "" {
			return event, false
		}
		c.legacyStreams[data.ChannelID] = data.PublisherConnectionID
	case StreamWatchingData:
		if c.legacyStreams == nil {
			c.legacyStreams = make(map[int64]string)
		}
		c.legacyStreams[data.Stream.ChannelID] = data.Stream.PublisherConnectionID
	case StreamStoppedData:
		if event.Type != EventStreamStopped {
			return event, true
		}
		if id, exists := c.legacyStreams[data.ChannelID]; exists && data.PublisherConnectionID != "" && data.PublisherConnectionID != id {
			return event, false
		}
		delete(c.legacyStreams, data.ChannelID)
	}
	return event, true
}

// A legacy tab can move to the next primary after its publisher stops, while
// newer clients receive the complete catalog and identity-specific events.
func (h *Hub) announceLegacyStreamReplacement(serverID, channelID int64) {
	for _, value := range h.streams.snapshotForServers([]int64{serverID}) {
		if value.ChannelID != channelID {
			continue
		}
		for _, client := range h.clientsForServer(serverID) {
			if !client.SupportsMultipleStreams() {
				h.SendToConnection(client.ConnectionID(), OutgoingEvent{Type: EventStreamStarted, Data: value})
			}
		}
		break
	}
}
