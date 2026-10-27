package realtime

import (
	"log"
	"slices"
	"time"
	"voxhold-backend/internal/telemetry"

	"voxhold-backend/internal/recordings"
)

// SetChannelAccessBatchLoader is configured together with the single-user
// loader used at registration, before connections are accepted.
func (h *Hub) SetChannelAccessBatchLoader(loader func([]int64) (map[int64]map[int64]struct{}, error)) {
	h.channelAccessBatchLoader = loader
}

const EventChannelsChanged EventType = "channels.changed"

// SetChannelAccessLoader must be configured before accepting connections.
// The cached snapshot is refreshed synchronously after access changes.
func (h *Hub) SetChannelAccessLoader(loader func(int64) (map[int64]struct{}, error)) {
	h.channelAccessLoader = loader
}

// LockChannelOperation covers signalling plus SFU setup, so revocation cannot
// close a session just before a delayed media Join creates it again.
func (h *Hub) LockChannelOperation() func() {
	h.channelAccessMu.RLock()
	return h.channelAccessMu.RUnlock
}

func (h *Hub) loadChannelAccess(userID int64) (map[int64]struct{}, error) {
	if h.channelAccessLoader == nil {
		return nil, nil
	}
	ids, err := h.channelAccessLoader(userID)
	if err != nil {
		log.Printf("load channel permissions user_id=%d: %v", userID, err)
		return map[int64]struct{}{}, err
	}
	if ids == nil {
		return map[int64]struct{}{}, nil
	}
	return ids, nil
}

func (h *Hub) RefreshChannelAccess(serverID int64) {
	start := time.Now()
	defer func() { telemetry.Observe(telemetry.ACLRefresh, time.Since(start), false) }()
	if h.channelAccessLoader == nil && h.channelAccessBatchLoader == nil {
		return
	}
	h.channelAccessMu.Lock()
	defer h.channelAccessMu.Unlock()
	clients := h.clientsForServer(serverID)
	failed := h.reloadChannelAccess(clients)
	for _, client := range clients {
		if failed[client.UserID()] {
			// Reconnect fetches a fresh snapshot after a transient database error.
			client.CloseWithReason(CloseReasonServerShutdown)
			h.Unregister(client)
			continue
		}
		unlock := h.mediaTransitions.Lock(client.UserID())
		if current, ok := h.voice.current(client); ok && !client.canAccessChannel(current.ChannelID) {
			h.leaveVoice(client)
			h.SendToConnection(client.ConnectionID(), OutgoingEvent{Type: EventVoiceWebRTCClosed, Data: VoiceWebRTCClosedData{Reason: "channel access was revoked"}})
		}
		unlock()
		for _, id := range client.subscriptionIDsForServer(serverID) {
			if !client.canAccessChannel(id) {
				h.Unsubscribe(client, id)
			}
		}
		h.sendVoiceSnapshot(client)
		h.sendStreamSnapshot(client)
	}
	// No hidden channel IDs or names are sent to unauthorized members.
	h.PublishToServer(serverID, OutgoingEvent{Type: EventChannelsChanged, Data: ServerDeletedData{ServerID: serverID}})
}

// Caller holds channelAccessMu exclusively. Install every client's policy before
// emitting cleanup events, which can include snapshots of other participants.
func (h *Hub) reloadChannelAccess(clients []*Client) map[int64]bool {
	permissions := make(map[int64]map[int64]struct{})
	failed := make(map[int64]bool)
	if h.channelAccessBatchLoader != nil {
		ids := make([]int64, 0, len(clients))
		seen := make(map[int64]bool)
		for _, client := range clients {
			if !seen[client.UserID()] {
				seen[client.UserID()] = true
				ids = append(ids, client.UserID())
			}
		}
		loaded, err := h.channelAccessBatchLoader(ids)
		if err != nil {
			log.Printf("load channel permission batch: %v", err)
		}
		for _, client := range clients {
			policy := loaded[client.UserID()]
			if err != nil || policy == nil {
				policy = map[int64]struct{}{}
			}
			failed[client.UserID()] = err != nil
			client.replaceChannelAccess(policy)
		}
		return failed
	}
	for _, client := range clients {
		ids, loaded := permissions[client.UserID()]
		if !loaded {
			var err error
			ids, err = h.loadChannelAccess(client.UserID())
			failed[client.UserID()] = err != nil
			permissions[client.UserID()] = ids
		}
		client.replaceChannelAccess(ids)
	}
	return failed
}

func (c *Client) canAccessChannel(channelID int64) bool {
	c.channelAccessMu.RLock()
	defer c.channelAccessMu.RUnlock()
	if c.channelAccess == nil {
		return true
	} // Hub without an installed policy (unit fixtures).
	_, ok := c.channelAccess[channelID]
	return ok
}

func (c *Client) replaceChannelAccess(ids map[int64]struct{}) {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()
	c.channelAccessMu.Lock()
	c.channelAccess = ids
	c.channelAccessMu.Unlock()
	// Remove queued private data as well as preventing future enqueues.
	for _, queue := range []*[]OutgoingEvent{&c.outgoing, &c.priority} {
		filtered := (*queue)[:0]
		for _, event := range *queue {
			if value, ok := c.filterChannelEvent(event); ok {
				filtered = append(filtered, value)
			}
		}
		clear((*queue)[len(filtered):])
		*queue = filtered
	}
}

func (c *Client) filterChannelEvent(event OutgoingEvent) (OutgoingEvent, bool) {
	var id int64
	switch data := event.Data.(type) {
	case interface{ EventChannelID() int64 }:
		id = data.EventChannelID()
	case ChannelData:
		id = data.ID
	case ChannelDeletedData:
		id = data.ChannelID
	case ChannelSubscriptionData:
		id = data.ChannelID
	case ChannelReadData:
		id = data.ChannelID
	case ChannelReadSnapshotData:
		id = data.ChannelID
	case MessageCreatedData:
		id = data.ChannelID
	case MessageDeletedData:
		id = data.ChannelID
	case MessagePinnedData:
		id = data.ChannelID
	case MessageUnpinnedData:
		id = data.ChannelID
	case VoiceParticipantData:
		id = data.ChannelID
	case VoiceLeftData:
		id = data.ChannelID
	case VoiceJoinedData:
		id = data.Participant.ChannelID
	case StreamData:
		id = data.ChannelID
	case StreamStoppedData:
		id = data.ChannelID
	case StreamWatchingData:
		id = data.Stream.ChannelID
	case recordings.Record:
		id = data.ChannelID
	case VoiceSnapshotData:
		data.Participants = slices.DeleteFunc(slices.Clone(data.Participants), func(p VoiceParticipantData) bool { return !c.canAccessChannel(p.ChannelID) })
		event.Data = data
	case StreamSnapshotData:
		data.Streams = slices.DeleteFunc(slices.Clone(data.Streams), func(s StreamData) bool { return !c.canAccessChannel(s.ChannelID) })
		event.Data = data
	case ReadSnapshotData:
		data.Reads = slices.DeleteFunc(slices.Clone(data.Reads), func(r ChannelReadData) bool { return !c.canAccessChannel(r.ChannelID) })
		event.Data = data
	}
	return event, id == 0 || c.canAccessChannel(id)
}
