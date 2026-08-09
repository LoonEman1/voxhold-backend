package realtime

import "sync"

const outgoingBufferSize = 128

type Client struct {
	userID int64

	outgoing  chan OutgoingEvent
	done      chan struct{}
	closeOnce sync.Once

	subscriptionsMu sync.RWMutex
	subscriptions   map[int64]struct{}
}

func NewClient(userID int64) *Client {
	return &Client{
		userID:        userID,
		outgoing:      make(chan OutgoingEvent, outgoingBufferSize),
		done:          make(chan struct{}),
		subscriptions: make(map[int64]struct{}),
	}
}

func (c *Client) UserID() int64 {
	return c.userID
}

func (c *Client) Outgoing() <-chan OutgoingEvent {
	return c.outgoing
}

func (c *Client) Done() <-chan struct{} {
	return c.done
}

func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
	})
}

func (c *Client) enqueue(
	event OutgoingEvent,
) bool {
	select {
	case <-c.done:
		return false
	default:
	}

	select {
	case c.outgoing <- event:
		return true

	case <-c.done:
		return false

	default:
		return false
	}
}

func (c *Client) addSubscription(
	channelID int64,
) {
	c.subscriptionsMu.Lock()
	defer c.subscriptionsMu.Unlock()

	c.subscriptions[channelID] = struct{}{}
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

func (c *Client) Send(
	event OutgoingEvent,
) bool {
	return c.enqueue(event)
}
