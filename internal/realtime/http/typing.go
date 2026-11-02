package realtimehttp

import (
	"encoding/json"
	"sync"
	"time"

	"voxhold-backend/internal/realtime"
)

const (
	typingMinInterval = 3 * time.Second
	typingLifetime    = 6 * time.Second
)

// typingLimiter drops typing.start events that arrive more often than once per
// interval for the same user and channel.
type typingLimiter struct {
	mu   sync.Mutex
	last map[[2]int64]time.Time
}

func (l *typingLimiter) allow(userID, channelID int64, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.last == nil {
		l.last = make(map[[2]int64]time.Time)
	}
	key := [2]int64{userID, channelID}
	if previous, ok := l.last[key]; ok && now.Sub(previous) < typingMinInterval {
		return false
	}
	if len(l.last) > 4096 {
		for k, t := range l.last {
			if now.Sub(t) > typingMinInterval {
				delete(l.last, k)
			}
		}
	}
	l.last[key] = now
	return true
}

// typingStart relays an ephemeral indicator to other subscribers. Invalid or
// too frequent events are ignored silently, as are unsubscribed channels.
func (h *Handler) typingStart(client *realtime.Client, event realtime.IncomingEvent) error {
	var data realtime.TypingData
	if err := json.Unmarshal(event.Data, &data); err != nil || data.ServerID <= 0 || data.ChannelID <= 0 {
		return nil
	}
	if !client.IsSubscribed(data.ServerID, data.ChannelID) || !h.typing.allow(client.UserID(), data.ChannelID, time.Now()) {
		return nil
	}
	h.hub.PublishExcept(data.ChannelID, realtime.OutgoingEvent{
		Type: realtime.EventTyping,
		Data: realtime.TypingData{
			ServerID:  data.ServerID,
			ChannelID: data.ChannelID,
			UserID:    client.UserID(),
			ExpiresAt: time.Now().Add(typingLifetime).Unix(),
		},
	}, client)
	return nil
}
