package realtime

import (
	"slices"
	"sync"
)

type room struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}
	closed  bool
}

func newRoom() *room {
	return &room{
		clients: make(map[*Client]struct{}),
	}
}

func (r *room) add(client *Client) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return false
	}

	if _, exists := r.clients[client]; exists {
		return false
	}

	r.clients[client] = struct{}{}

	return true
}

func (r *room) closeAndSnapshot() []*Client {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	r.closed = true
	clients := make(
		[]*Client,
		0,
		len(r.clients),
	)

	for client := range r.clients {
		clients = append(clients, client)
	}

	clear(r.clients)

	return clients
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

	clientsMu           sync.RWMutex
	clients             map[*Client]struct{}
	clientsByUser       map[int64]map[*Client]struct{}
	clientsBySession    map[[32]byte]map[*Client]struct{}
	clientsByConnection map[string]*Client

	presenceMu sync.RWMutex
	presence   map[int64]map[int64]int

	voice         *voiceState
	voiceSessions VoiceSessionCloser
}

func NewHub() *Hub {
	return &Hub{
		rooms:               make(map[int64]*room),
		clients:             make(map[*Client]struct{}),
		clientsByUser:       make(map[int64]map[*Client]struct{}),
		clientsBySession:    make(map[[32]byte]map[*Client]struct{}),
		clientsByConnection: make(map[string]*Client),
		presence:            make(map[int64]map[int64]int),
		voice:               newVoiceState(),
	}
}

func (h *Hub) Register(client *Client) bool {
	if client == nil || client.UserID() <= 0 {
		return false
	}

	h.clientsMu.Lock()

	select {
	case <-client.Done():
		h.clientsMu.Unlock()
		return false
	default:
	}

	if _, exists := h.clients[client]; exists {
		h.clientsMu.Unlock()
		return false
	}

	h.clients[client] = struct{}{}
	h.clientsByConnection[client.ConnectionID()] = client

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
	h.clientsMu.Unlock()

	h.registerPresence(client)
	h.sendVoiceSnapshot(client)

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
	h.leaveVoice(client)

	for _, channelID := range client.subscriptionIDs() {
		h.Unsubscribe(client, channelID)
	}

	h.clientsMu.Lock()
	delete(h.clients, client)
	delete(h.clientsByConnection, client.ConnectionID())

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
	h.clientsMu.Unlock()

	h.unregisterPresence(client)
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

	serverClients := h.clientsForServer(serverID)

	h.clientsMu.RLock()
	userClients := clientSetSnapshot(
		h.clientsByUser[userID],
	)
	h.clientsMu.RUnlock()

	h.broadcastToClients(
		append(serverClients, userClients...),
		OutgoingEvent{
			Type: EventServerMemberRemoved,
			Data: ServerMemberRemovedData{
				ServerID: serverID,
				UserID:   userID,
			},
		},
	)

	for _, client := range userClients {
		h.leaveVoiceForServer(client, serverID)
		h.removeClientServer(client, serverID, false)
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

	clients := h.clientsForServer(serverID)

	h.broadcastToClients(
		clients,
		OutgoingEvent{
			Type: EventServerDeleted,
			Data: ServerDeletedData{
				ServerID: serverID,
			},
		},
	)

	for _, client := range clients {
		h.removeClientServer(client, serverID, false)
		h.unsubscribeFromServer(
			client,
			serverID,
		)
	}

	h.presenceMu.Lock()
	delete(h.presence, serverID)
	h.presenceMu.Unlock()

	for _, participant := range h.voice.removeServer(serverID) {
		h.closeVoiceSession(participant.ConnectionID)
	}
}

func (h *Hub) clientsForServer(
	serverID int64,
) []*Client {
	h.clientsMu.RLock()
	clients := clientSetSnapshot(h.clients)
	h.clientsMu.RUnlock()

	serverClients := make(
		[]*Client,
		0,
		len(clients),
	)

	for _, client := range clients {
		if client.hasServer(serverID) {
			serverClients = append(
				serverClients,
				client,
			)
		}

	}

	return serverClients
}

func (h *Hub) AddUserToServer(
	userID int64,
	serverID int64,
) {
	if userID <= 0 || serverID <= 0 {
		return
	}

	h.clientsMu.RLock()
	clients := clientSetSnapshot(h.clientsByUser[userID])
	h.clientsMu.RUnlock()

	for _, client := range clients {
		if !client.addServer(serverID) {
			continue
		}

		becameOnline := h.incrementPresence(
			serverID,
			userID,
		)
		if becameOnline {
			h.broadcastPresence(
				serverID,
				userID,
				PresenceOnline,
			)
		}

		h.sendPresenceSnapshot(client, []int64{serverID})
	}
}

func (h *Hub) registerPresence(client *Client) {
	serverIDs := client.serverIDs()

	for _, serverID := range serverIDs {
		if h.incrementPresence(serverID, client.UserID()) {
			h.broadcastPresence(
				serverID,
				client.UserID(),
				PresenceOnline,
			)
		}
	}

	h.sendPresenceSnapshot(client, serverIDs)
}

func (h *Hub) unregisterPresence(client *Client) {
	for _, serverID := range client.serverIDs() {
		h.removeClientServer(client, serverID, true)
	}
}

func (h *Hub) removeClientServer(
	client *Client,
	serverID int64,
	announce bool,
) {
	if !client.removeServer(serverID) {
		return
	}

	becameOffline := h.decrementPresence(
		serverID,
		client.UserID(),
	)
	if announce && becameOffline {
		h.broadcastPresence(
			serverID,
			client.UserID(),
			PresenceOffline,
		)
	}
}

func (h *Hub) incrementPresence(
	serverID int64,
	userID int64,
) bool {
	h.presenceMu.Lock()
	defer h.presenceMu.Unlock()

	serverPresence := h.presence[serverID]
	if serverPresence == nil {
		serverPresence = make(map[int64]int)
		h.presence[serverID] = serverPresence
	}

	wasOffline := serverPresence[userID] == 0
	serverPresence[userID]++

	return wasOffline
}

func (h *Hub) decrementPresence(
	serverID int64,
	userID int64,
) bool {
	h.presenceMu.Lock()
	defer h.presenceMu.Unlock()

	serverPresence := h.presence[serverID]
	if serverPresence == nil || serverPresence[userID] == 0 {
		return false
	}

	serverPresence[userID]--
	if serverPresence[userID] > 0 {
		return false
	}

	delete(serverPresence, userID)
	if len(serverPresence) == 0 {
		delete(h.presence, serverID)
	}

	return true
}

func (h *Hub) onlineUserIDs(serverID int64) []int64 {
	h.presenceMu.RLock()
	defer h.presenceMu.RUnlock()

	serverPresence := h.presence[serverID]
	userIDs := make([]int64, 0, len(serverPresence))

	for userID, connections := range serverPresence {
		if connections > 0 {
			userIDs = append(userIDs, userID)
		}
	}

	slices.Sort(userIDs)

	return userIDs
}

func (h *Hub) sendPresenceSnapshot(
	client *Client,
	serverIDs []int64,
) {
	servers := make(
		[]ServerPresenceSnapshotData,
		0,
		len(serverIDs),
	)

	slices.Sort(serverIDs)

	for _, serverID := range serverIDs {
		servers = append(
			servers,
			ServerPresenceSnapshotData{
				ServerID: serverID,
				OnlineUserIDs: h.onlineUserIDs(
					serverID,
				),
			},
		)
	}

	if !client.enqueue(
		OutgoingEvent{
			Type: EventPresenceSnapshot,
			Data: PresenceSnapshotData{
				Servers: servers,
			},
		},
	) {
		h.Unregister(client)
	}
}

func (h *Hub) broadcastPresence(
	serverID int64,
	userID int64,
	status PresenceStatus,
) {
	h.broadcastToClients(
		h.clientsForServer(serverID),
		OutgoingEvent{
			Type: EventPresenceUpdated,
			Data: PresenceUpdatedData{
				ServerID: serverID,
				UserID:   userID,
				Status:   status,
			},
		},
	)
}

func (h *Hub) broadcastToClients(
	clients []*Client,
	event OutgoingEvent,
) int {
	delivered := make(
		map[*Client]struct{},
		len(clients),
	)
	deliveredCount := 0

	for _, client := range clients {
		if _, exists := delivered[client]; exists {
			continue
		}

		delivered[client] = struct{}{}

		if !client.enqueue(event) {
			h.Unregister(client)
			continue
		}

		deliveredCount++
	}

	return deliveredCount
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

func (h *Hub) PublishToServer(
	serverID int64,
	event OutgoingEvent,
) int {
	if serverID <= 0 {
		return 0
	}

	clients := h.clientsForServer(serverID)
	return h.broadcastToClients(clients, event)
}

func (h *Hub) PublishToUser(
	userID int64,
	event OutgoingEvent,
) int {
	if userID <= 0 {
		return 0
	}

	h.clientsMu.RLock()
	clients := clientSetSnapshot(h.clientsByUser[userID])
	h.clientsMu.RUnlock()

	return h.broadcastToClients(clients, event)
}

func (h *Hub) PublishToChannelAndUser(
	channelID int64,
	userID int64,
	event OutgoingEvent,
) int {
	if channelID <= 0 || userID <= 0 {
		return 0
	}

	var clients []*Client

	if room := h.getRoom(channelID); room != nil {
		clients = room.snapshot()
	}

	h.clientsMu.RLock()
	clients = append(
		clients,
		clientSetSnapshot(h.clientsByUser[userID])...,
	)
	h.clientsMu.RUnlock()

	return h.broadcastToClients(clients, event)
}

func (h *Hub) RemoveChannel(channelID int64) {
	if channelID <= 0 {
		return
	}

	h.roomsMu.Lock()
	room := h.rooms[channelID]
	if room == nil {
		h.roomsMu.Unlock()
		for _, participant := range h.voice.removeChannel(channelID) {
			h.closeVoiceSession(participant.ConnectionID)
		}
		return
	}
	delete(h.rooms, channelID)
	h.roomsMu.Unlock()

	for _, client := range room.closeAndSnapshot() {
		client.removeSubscription(channelID)
	}

	for _, participant := range h.voice.removeChannel(channelID) {
		h.closeVoiceSession(participant.ConnectionID)
	}
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
