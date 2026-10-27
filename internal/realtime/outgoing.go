package realtime

import (
	"fmt"
	"strings"
)

const EventChannelSyncRequired EventType = "channel.sync_required"

type QueueStats struct {
	NormalPending int
	MediaPending  int
	HighWatermark int
	Coalesced     uint64
	Resyncs       uint64
}

func (c *Client) QueueStats() QueueStats {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()
	return QueueStats{len(c.outgoing), len(c.priority), c.queueHighWater, c.queueCoalesced, c.queueResyncs}
}

func (c *Client) notifyOutgoing() {
	select {
	case c.outgoingReady <- struct{}{}:
	default:
	}
}

// PopOutgoing preserves FIFO within each lane and gives ordinary events a turn
// after eight media events. No goroutine or unbounded per-client buffer is used.
func (c *Client) PopOutgoing() (OutgoingEvent, bool) {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()
	queue := &c.outgoing
	if len(c.priority) > 0 && (c.priorityBurst < 8 || len(c.outgoing) == 0) {
		queue = &c.priority
		c.priorityBurst++
	} else {
		c.priorityBurst = 0
	}
	if len(*queue) == 0 {
		return OutgoingEvent{}, false
	}
	event := (*queue)[0]
	copy(*queue, (*queue)[1:])
	(*queue)[len(*queue)-1] = OutgoingEvent{}
	*queue = (*queue)[:len(*queue)-1]
	if len(c.outgoing)+len(c.priority) > 0 {
		c.notifyOutgoing()
	}
	return event, true
}

func (c *Client) enqueueLocked(event OutgoingEvent) bool {
	if strings.HasPrefix(string(event.Type), "voice.") || strings.HasPrefix(string(event.Type), "stream.") {
		if len(c.priority) == outgoingBufferSize {
			return false
		}
		c.priority = append(c.priority, event)
		return true
	}
	if key := replacementKey(event); key != "" {
		for index, queued := range c.outgoing {
			if replacementKey(queued) == key {
				if previous, ok := queued.Data.(ChannelReadData); ok {
					if next := event.Data.(ChannelReadData); next.LastReadMessageID < previous.LastReadMessageID {
						c.queueCoalesced++
						return true
					}
				}
				c.outgoing[index] = event
				c.queueCoalesced++
				return true
			}
		}
	}
	channelID := eventMessageChannel(event)
	if channelID > 0 {
		for _, queued := range c.outgoing {
			if queued.Type == EventChannelSyncRequired && queued.Data.(ChannelSubscriptionData).ChannelID == channelID {
				c.queueCoalesced++
				return true
			}
		}
	}
	if len(c.outgoing) == outgoingBufferSize {
		// Replace queued chat deltas with an explicit durable catch-up request.
		// Permission changes, SDP and ICE are never silently dropped.
		for _, queued := range c.outgoing {
			if candidate := eventMessageChannel(queued); candidate > 0 {
				if !c.compactChannel(candidate) {
					continue
				}
				if candidate == channelID {
					return true
				}
				// Replacing one delta with one marker may not free a slot.
				// Keep searching other channels before rejecting a critical event.
				if len(c.outgoing) < outgoingBufferSize {
					break
				}
			}
		}
	}
	if len(c.outgoing) == outgoingBufferSize {
		return false
	}
	c.outgoing = append(c.outgoing, event)
	return true
}

func (c *Client) compactChannel(channelID int64) bool {
	if !c.messageSync.Load() {
		return false
	}
	c.subscriptionsMu.RLock()
	serverID := c.subscriptions[channelID]
	c.subscriptionsMu.RUnlock()
	if serverID <= 0 {
		return false
	}
	kept := c.outgoing[:0]
	inserted := false
	for _, queued := range c.outgoing {
		if eventMessageChannel(queued) == channelID {
			if !inserted {
				kept = append(kept, OutgoingEvent{Type: EventChannelSyncRequired, Data: ChannelSubscriptionData{ServerID: serverID, ChannelID: channelID}})
				inserted = true
			}
			continue
		}
		kept = append(kept, queued)
	}
	clear(c.outgoing[len(kept):])
	c.outgoing = kept
	if inserted {
		c.queueResyncs++
	}
	return inserted
}

func replacementKey(event OutgoingEvent) string {
	if event.RequestID != "" {
		return ""
	}
	switch value := event.Data.(type) {
	case PresenceUpdatedData:
		if event.Type == EventPresenceUpdated {
			return fmt.Sprintf("presence/%d/%d", value.ServerID, value.UserID)
		}
	case ChannelReadData:
		if event.Type == EventChannelRead {
			return fmt.Sprintf("read/%d/%d", value.ChannelID, value.UserID)
		}
	}
	return ""
}

func eventMessageChannel(event OutgoingEvent) int64 {
	if event.RequestID != "" {
		return 0
	}
	switch event.Type {
	case EventMessageCreated, EventMessageUpdated:
		if value, ok := event.Data.(MessageCreatedData); ok {
			return value.ChannelID
		}
	case EventMessageDeleted, EventMessageUnpinned:
		if value, ok := event.Data.(MessageDeletedData); ok {
			return value.ChannelID
		}
	case EventMessagePinned:
		if value, ok := event.Data.(MessagePinnedData); ok {
			return value.ChannelID
		}
	}
	return 0
}
