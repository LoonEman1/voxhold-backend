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

	clientsMu        sync.RWMutex
	clients          map[*Client]struct{}
	clientsByUser    map[int64]map[*Client]struct{}
	clientsBySession map[[32]byte]map[*Client]struct{}
}

func NewHub() *Hub {
	return &Hub{
		rooms:            make(map[int64]*room),
		clients:          make(map[*Client]struct{}),
		clientsByUser:    make(map[int64]map[*Client]struct{}),
		clientsBySession: make(map[[32]byte]map[*Client]struct{}),
	}
}

func (h *Hub) Register(client *Client) bool {
	if client == nil || client.UserID() <= 0 {
		return false
	}

	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()

	select {
	case <-client.Done():
		return false
	default:
	}

	if _, exists := h.clients[client]; exists {
		return false
	}

	h.clients[client] = struct{}{}

	userClients := h.clientsByUser[client.userID]
	if userClients == nil {
		userClients = make(map[*Client]struct{})
		h.clientsByUser[client.userID] = userClients
	}
	userClients[client] = struct{}{}

	sessionClients := h.clientsBySession[client.sessionKey]
	if sessionClients == nil {
		sessionClients = make(map[*Client]struct{})
		h.clientsBySession[client.sessionKey] = sessionClients
	}
	sessionClients[client] = struct{}{}

	return true
}

func (h *Hub) Subscribe(
	client *Client,
	serverID int64,
	channelID int64,
) bool {
	if client == nil || serverID <= 0 || channelID <= 0 {
		return false
	}

	room := h.getOrCreateRoom(channelID)

	added := room.add(client)
	if !added {
		return false
	}

	client.addSubscription(serverID, channelID)

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

	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()

	delete(h.clients, client)

	userClients := h.clientsByUser[client.userID]
	delete(userClients, client)
	if len(userClients) == 0 {
		delete(h.clientsByUser, client.userID)
	}

	sessionClients := h.clientsBySession[client.sessionKey]
	delete(sessionClients, client)
	if len(sessionClients) == 0 {
		delete(h.clientsBySession, client.sessionKey)
	}
}

func (h *Hub) RevokeSession(token string) {
	key := newSessionKey(token)

	h.clientsMu.RLock()
	clients := clientSetSnapshot(
		h.clientsBySession[key],
	)
	h.clientsMu.RUnlock()

	for _, client := range clients {
		client.CloseWithReason(
			"session expired or revoked",
		)
		h.Unregister(client)
	}
}

func (h *Hub) RevokeUserFromServer(
	userID int64,
	serverID int64,
) {
	if userID <= 0 || serverID <= 0 {
		return
	}

	h.clientsMu.RLock()
	clients := clientSetSnapshot(
		h.clientsByUser[userID],
	)
	h.clientsMu.RUnlock()

	for _, client := range clients {
		h.unsubscribeFromServer(
			client,
			serverID,
		)
	}
}

func (h *Hub) RevokeServer(serverID int64) {
	if serverID <= 0 {
		return
	}

	h.clientsMu.RLock()
	clients := clientSetSnapshot(h.clients)
	h.clientsMu.RUnlock()

	for _, client := range clients {
		h.unsubscribeFromServer(
			client,
			serverID,
		)
	}
}

func (h *Hub) unsubscribeFromServer(
	client *Client,
	serverID int64,
) {
	for _, channelID := range client.subscriptionIDsForServer(serverID) {

		h.Unsubscribe(client, channelID)
	}
}

func clientSetSnapshot(
	values map[*Client]struct{},
) []*Client {
	clients := make(
		[]*Client,
		0,
		len(values),
	)

	for client := range values {
		clients = append(clients, client)
	}

	return clients
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
