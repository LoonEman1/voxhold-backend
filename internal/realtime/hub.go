package realtime

import "sync"

type room struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}
}

func newRoom() *room {
	return &room{
		clients: make(map[*Client]struct{}),
	}
}

func (r *room) add(client *Client) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.clients[client]; exists {
		return false
	}

	r.clients[client] = struct{}{}

	return true
}

func (r *room) remove(client *Client) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.clients[client]; !exists {
		return false
	}

	delete(r.clients, client)

	return true
}

func (r *room) snapshot() []*Client {
	r.mu.RLock()
	defer r.mu.RUnlock()

	clients := make(
		[]*Client,
		0,
		len(r.clients),
	)

	for client := range r.clients {
		clients = append(
			clients,
			client,
		)
	}

	return clients
}

type Hub struct {
	roomsMu sync.RWMutex
	rooms   map[int64]*room
}

func NewHub() *Hub {
	return &Hub{
		rooms: make(map[int64]*room),
	}
}

func (h *Hub) Subscribe(
	client *Client,
	channelID int64,
) bool {
	if client == nil || channelID <= 0 {
		return false
	}

	room := h.getOrCreateRoom(channelID)

	added := room.add(client)
	if !added {
		return false
	}

	client.addSubscription(channelID)

	return true
}

func (h *Hub) Unsubscribe(
	client *Client,
	channelID int64,
) bool {
	if client == nil || channelID <= 0 {
		return false
	}

	room := h.getRoom(channelID)
	if room == nil {
		client.removeSubscription(channelID)
		return false
	}

	removed := room.remove(client)
	client.removeSubscription(channelID)

	return removed
}

func (h *Hub) Unregister(client *Client) {
	if client == nil {
		return
	}

	client.Close()

	for _, channelID := range client.subscriptionIDs() {
		h.Unsubscribe(client, channelID)
	}
}

func (h *Hub) Publish(
	channelID int64,
	event OutgoingEvent,
) int {
	if channelID <= 0 {
		return 0
	}

	room := h.getRoom(channelID)
	if room == nil {
		return 0
	}

	clients := room.snapshot()
	delivered := 0

	for _, client := range clients {
		if client.enqueue(event) {
			delivered++
			continue
		}

		h.Unregister(client)
	}

	return delivered
}

func (h *Hub) getRoom(
	channelID int64,
) *room {
	h.roomsMu.RLock()
	defer h.roomsMu.RUnlock()

	return h.rooms[channelID]
}

func (h *Hub) getOrCreateRoom(
	channelID int64,
) *room {
	h.roomsMu.RLock()
	room := h.rooms[channelID]
	h.roomsMu.RUnlock()

	if room != nil {
		return room
	}

	h.roomsMu.Lock()
	defer h.roomsMu.Unlock()

	room = h.rooms[channelID]
	if room != nil {
		return room
	}

	room = newRoom()
	h.rooms[channelID] = room

	return room
}
