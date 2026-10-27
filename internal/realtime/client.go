package realtime

import (
	"crypto/sha256"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"voxhold-backend/internal/telemetry"
)

const outgoingBufferSize = 128

var (
	nextConnectionID        atomic.Uint64
	websocketQueueFullTotal atomic.Uint64
)

type Client struct {
	channelAccessMu sync.RWMutex
	channelAccess   map[int64]struct{}
	connectionID    string
	userID          int64
	sessionKey      [sha256.Size]byte

	queueMu         sync.Mutex
	outgoing        []OutgoingEvent
	priority        []OutgoingEvent
	outgoingReady   chan struct{}
	priorityBurst   int
	queueCoalesced  uint64
	queueResyncs    uint64
	queueHighWater  int
	messageSync     atomic.Bool
	done            chan struct{}
	closeOnce       sync.Once
	stateMu         sync.RWMutex
	closeReason     string
	queueFullLogged atomic.Bool

	subscriptionsMu sync.RWMutex
	subscriptions   map[int64]int64

	serversMu sync.RWMutex
	servers   map[int64]struct{}
}

func NewClient(
	userID int64,
	sessionToken string,
	serverIDs []int64,
) *Client {
	client := &Client{
		connectionID:  newConnectionID(),
		userID:        userID,
		sessionKey:    newSessionKey(sessionToken),
		outgoing:      make([]OutgoingEvent, 0, outgoingBufferSize),
		priority:      make([]OutgoingEvent, 0, outgoingBufferSize),
		outgoingReady: make(chan struct{}, 1),
		done:          make(chan struct{}),
		subscriptions: make(map[int64]int64),
		servers:       make(map[int64]struct{}, len(serverIDs)),
	}

	for _, serverID := range serverIDs {
		if serverID > 0 {
			client.servers[serverID] = struct{}{}
		}
	}

	return client
}

func newConnectionID() string {
	return strconv.FormatUint(
		nextConnectionID.Add(1),
		36,
	)
}

func newSessionKey(token string) [sha256.Size]byte {
	return sha256.Sum256(
		[]byte(strings.TrimSpace(token)),
	)
}

func (c *Client) UserID() int64 {
	return c.userID
}

func (c *Client) EnableMessageSync() { c.messageSync.Store(true) }

func (c *Client) ConnectionID() string {
	return c.connectionID
}

func (c *Client) OutgoingReady() <-chan struct{} {
	return c.outgoingReady
}

func (c *Client) Done() <-chan struct{} {
	return c.done
}

func (c *Client) Close() {
	c.CloseWithReason("")
}

func (c *Client) CloseWithReason(reason string) {
	c.closeOnce.Do(func() {
		c.stateMu.Lock()
		defer c.stateMu.Unlock()

		c.closeReason = reason
		close(c.done)
	})
}

func (c *Client) CloseReason() string {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()

	return c.closeReason
}

func (c *Client) enqueue(
	event OutgoingEvent,
) bool {
	if telemetry.Enabled() {
		event.queuedAt = time.Now()
	}
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()

	select {
	case <-c.done:
		return false
	default:
	}

	c.queueMu.Lock()
	defer c.queueMu.Unlock()
	var allowed bool
	event, allowed = c.filterChannelEvent(event)
	if !allowed {
		return true
	}
	accepted := c.enqueueLocked(event)
	if accepted {
		pending := len(c.outgoing) + len(c.priority)
		if pending > c.queueHighWater {
			c.queueHighWater = pending
		}
		c.notifyOutgoing()
		return true
	}
	if c.queueFullLogged.CompareAndSwap(false, true) {
		total := websocketQueueFullTotal.Add(1)
		log.Printf("event=websocket_outgoing_queue_full total=%d connection_id=%s user_id=%d normal_pending=%d media_pending=%d", total, c.connectionID, c.userID, len(c.outgoing), len(c.priority))
	}
	return false
}

func (c *Client) addSubscription(
	serverID int64,
	channelID int64,
) {
	c.subscriptionsMu.Lock()
	defer c.subscriptionsMu.Unlock()

	c.subscriptions[channelID] = serverID
}

func (c *Client) removeSubscription(
	channelID int64,
) {
	c.subscriptionsMu.Lock()
	defer c.subscriptionsMu.Unlock()

	delete(c.subscriptions, channelID)
}

func (c *Client) subscriptionIDs() []int64 {
	c.subscriptionsMu.RLock()
	defer c.subscriptionsMu.RUnlock()

	channelIDs := make(
		[]int64,
		0,
		len(c.subscriptions),
	)

	for channelID := range c.subscriptions {
		channelIDs = append(
			channelIDs,
			channelID,
		)
	}

	return channelIDs
}

func (c *Client) subscriptionIDsForServer(
	serverID int64,
) []int64 {
	c.subscriptionsMu.RLock()
	defer c.subscriptionsMu.RUnlock()

	channelIDs := make([]int64, 0)

	for channelID, subscribedServerID := range c.subscriptions {

		if subscribedServerID == serverID {
			channelIDs = append(
				channelIDs,
				channelID,
			)
		}
	}

	return channelIDs
}

func (c *Client) hasSubscriptionForServer(
	serverID int64,
) bool {
	c.subscriptionsMu.RLock()
	defer c.subscriptionsMu.RUnlock()

	for _, subscribedServerID := range c.subscriptions {
		if subscribedServerID == serverID {
			return true
		}
	}

	return false
}

func (c *Client) serverIDs() []int64 {
	c.serversMu.RLock()
	defer c.serversMu.RUnlock()

	serverIDs := make([]int64, 0, len(c.servers))
	for serverID := range c.servers {
		serverIDs = append(serverIDs, serverID)
	}

	return serverIDs
}

func (c *Client) hasServer(serverID int64) bool {
	c.serversMu.RLock()
	defer c.serversMu.RUnlock()

	_, exists := c.servers[serverID]
	return exists
}

func (c *Client) addServer(serverID int64) bool {
	c.serversMu.Lock()
	defer c.serversMu.Unlock()

	if _, exists := c.servers[serverID]; exists {
		return false
	}

	c.servers[serverID] = struct{}{}
	return true
}

func (c *Client) removeServer(serverID int64) bool {
	c.serversMu.Lock()
	defer c.serversMu.Unlock()

	if _, exists := c.servers[serverID]; !exists {
		return false
	}

	delete(c.servers, serverID)
	return true
}

func (c *Client) Send(
	event OutgoingEvent,
) bool {
	return c.enqueue(event)
}
