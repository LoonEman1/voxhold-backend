package realtime

import (
	"crypto/sha256"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

const outgoingBufferSize = 128

var nextConnectionID atomic.Uint64

type Client struct {
	connectionID string
	userID       int64
	sessionKey   [sha256.Size]byte

	outgoing    chan OutgoingEvent
	done        chan struct{}
	closeOnce   sync.Once
	stateMu     sync.RWMutex
	closeReason string

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
		outgoing:      make(chan OutgoingEvent, outgoingBufferSize),
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

func (c *Client) ConnectionID() string {
	return c.connectionID
}

func (c *Client) Outgoing() <-chan OutgoingEvent {
	return c.outgoing
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
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()

	select {
	case <-c.done:
		return false
	default:
	}

	select {
	case c.outgoing <- event:
		return true

	default:
		return false
	}
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
