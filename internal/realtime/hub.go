package realtime

import (
	"slices"
	"sync"
	"time"
	"voxhold-backend/internal/keylock"
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
	rolePermissions          func(serverID, userID int64) (int, error)
	channelAccessMu          sync.RWMutex
	channelAccessLoader      func(int64) (map[int64]struct{}, error)
	channelAccessBatchLoader func([]int64) (map[int64]map[int64]struct{}, error)
	roomsMu                  sync.RWMutex
	rooms                    map[int64]*room

	clientsMu           sync.RWMutex
	closed              bool
	clients             map[*Client]struct{}
	clientsByUser       map[int64]map[*Client]struct{}
	clientsByServer     map[int64]map[*Client]struct{}
	clientsBySession    map[[32]byte]map[*Client]struct{}
	clientsByConnection map[string]*Client

	presenceMu         sync.RWMutex
	presence           map[int64]map[int64]int
	lastSeenSaver      func(userID, at int64)
	lastSeenMu         sync.Mutex
	lastSeen           map[int64]presenceStamp
	presenceBroadcasts keylock.Set

	mediaTransitions keylock.Set
	voice            *voiceState
	voiceSessions    VoiceSessionCloser

	streams             *streamState
	streamSessions      StreamSessionCloser
	maxStreamViewers    int
	maxP2PStreamViewers int
}

func NewHub() *Hub {
	return &Hub{
		rooms:           make(map[int64]*room),
		clients:         make(map[*Client]struct{}),
		clientsByUser:   make(map[int64]map[*Client]struct{}),
		clientsByServer: make(map[int64]map[*Client]struct{}), lastSeen: make(map[int64]presenceStamp),
		clientsBySession:    make(map[[32]byte]map[*Client]struct{}),
		clientsByConnection: make(map[string]*Client),
		presence:            make(map[int64]map[int64]int),
		voice:               newVoiceState(),
		streams:             newStreamState(),
		maxStreamViewers:    32,
		maxP2PStreamViewers: 8,
	}
}

// ConnectedUserIDs snapshots accounts once, regardless of their number of tabs.
func (h *Hub) ConnectedUserIDs() []int64 {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	ids := make([]int64, 0, len(h.clientsByUser))
	for id := range h.clientsByUser {
		ids = append(ids, id)
	}
	return ids
}

// IsUserOnline checks one account without allocating an instance-wide snapshot.
func (h *Hub) IsUserOnline(userID int64) bool {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	return len(h.clientsByUser[userID]) > 0
}

// CloseReasonServerShutdown marks connections closed by a server restart.
// The WebSocket layer sends it with 1012 (Service Restart) so clients
// reconnect and restore voice instead of treating it as an auth failure.
const CloseReasonServerShutdown = "server is shutting down"

func (h *Hub) Close() {
	h.clientsMu.Lock()
	h.closed = true
	clients := clientSetSnapshot(h.clients)
	h.clientsMu.Unlock()

	for _, client := range clients {
		client.CloseWithReason(CloseReasonServerShutdown)
		h.Unregister(client)
	}
}

func (h *Hub) Register(client *Client) bool {
	defer h.LockChannelOperation()()
	if client != nil {
		ids, err := h.loadChannelAccess(client.UserID())
		if err != nil {
			client.CloseWithReason(CloseReasonServerShutdown)
			return false
		}
		client.replaceChannelAccess(ids)
	}
	if client == nil || client.UserID() <= 0 {
		return false
	}

	h.clientsMu.Lock()
	if h.closed {
		h.clientsMu.Unlock()
		return false
	}

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
	for _, serverID := range client.serverIDs() {
		h.indexServerLocked(serverID, client)
	}
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
	h.sendStreamSnapshot(client)

	return true
}

func (h *Hub) Subscribe(
	client *Client,
	serverID int64,
	channelID int64,
) bool {
	defer h.LockChannelOperation()()
	if client == nil || serverID <= 0 || channelID <= 0 || !client.canAccessChannel(channelID) {
		return false
	}
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	if _, registered := h.clients[client]; !registered || h.closed || !client.hasServer(serverID) {
		return false
	}
	select {
	case <-client.Done():
		return false
	default:
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
	h.clientsMu.Lock()
	delete(h.clients, client)
	for _, serverID := range client.serverIDs() {
		h.unindexServerLocked(serverID, client)
	}
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

	// Detach the client first, waiting for any in-flight Subscribe to finish.
	// It can no longer add a subscription after the cleanup snapshot.
	h.leaveVoice(client)
	for _, channelID := range client.subscriptionIDs() {
		h.Unsubscribe(client, channelID)
	}

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
	h.channelAccessMu.Lock()
	defer h.channelAccessMu.Unlock()

	serverClients := h.clientsForServer(serverID)

	h.clientsMu.RLock()
	userClients := clientSetSnapshot(
		h.clientsByUser[userID],
	)
	h.clientsMu.RUnlock()
	failed := h.reloadChannelAccess(userClients)

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
		if failed[client.UserID()] {
			client.CloseWithReason(CloseReasonServerShutdown)
			h.Unregister(client)
			continue
		}
		unlock := h.mediaTransitions.Lock(client.UserID())
		h.leaveVoiceForServer(client, serverID)
		unlock()
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
	h.channelAccessMu.Lock()
	defer h.channelAccessMu.Unlock()

	clients := h.clientsForServer(serverID)
	failed := h.reloadChannelAccess(clients)

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
		if failed[client.UserID()] {
			client.CloseWithReason(CloseReasonServerShutdown)
			h.Unregister(client)
			continue
		}
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
		h.stopStreamForChannel(
			participant.ChannelID,
			"server was deleted",
		)
		h.closeVoiceSession(participant.ConnectionID)
	}
}

func (h *Hub) clientsForServer(serverID int64) []*Client {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	return clientSetSnapshot(h.clientsByServer[serverID])
}

// Caller holds clientsMu before touching the client's server membership.
func (h *Hub) indexServerLocked(serverID int64, client *Client) {
	clients := h.clientsByServer[serverID]
	if clients == nil {
		clients = make(map[*Client]struct{})
		h.clientsByServer[serverID] = clients
	}
	clients[client] = struct{}{}
}
func (h *Hub) unindexServerLocked(serverID int64, client *Client) {
	clients := h.clientsByServer[serverID]
	delete(clients, client)
	if len(clients) == 0 {
		delete(h.clientsByServer, serverID)
	}
}

func (h *Hub) AddUserToServer(
	userID int64,
	serverID int64,
) {
	defer h.LockChannelOperation()()
	if userID <= 0 || serverID <= 0 {
		return
	}

	h.clientsMu.RLock()
	clients := clientSetSnapshot(h.clientsByUser[userID])
	h.clientsMu.RUnlock()

	for _, client := range clients {
		h.clientsMu.Lock()
		_, registered := h.clients[client]
		if !registered || !client.addServer(serverID) {
			h.clientsMu.Unlock()
			continue
		}
		h.indexServerLocked(serverID, client)
		becameOnline := h.incrementPresence(serverID, userID)
		h.clientsMu.Unlock()
		ids, err := h.loadChannelAccess(userID)
		if err != nil {
			client.CloseWithReason(CloseReasonServerShutdown)
			h.Unregister(client)
			continue
		}
		client.replaceChannelAccess(ids)
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
	// Registration may race with disconnect before this post-registration step.
	h.clientsMu.RLock()
	if _, registered := h.clients[client]; !registered {
		h.clientsMu.RUnlock()
		return
	}
	serverIDs := client.serverIDs()
	var online []int64
	for _, serverID := range serverIDs {
		if h.incrementPresence(serverID, client.UserID()) {
			online = append(online, serverID)
		}
	}
	h.clientsMu.RUnlock()
	for _, serverID := range online {
		h.broadcastPresence(serverID, client.UserID(), PresenceOnline)
	}
	h.sendPresenceSnapshot(client, serverIDs)
}

func (h *Hub) unregisterPresence(client *Client) {
	for _, serverID := range client.serverIDs() {
		h.removeClientServer(client, serverID, true)
	}
	// Keep the offline stamp through the complete server batch, then reclaim it.
	h.clientsMu.RLock()
	if len(h.clientsByUser[client.UserID()]) == 0 {
		h.lastSeenMu.Lock()
		delete(h.lastSeen, client.UserID())
		h.lastSeenMu.Unlock()
	}
	h.clientsMu.RUnlock()
}

func (h *Hub) removeClientServer(
	client *Client,
	serverID int64,
	announce bool,
) {
	h.clientsMu.Lock()
	if !client.removeServer(serverID) {
		h.clientsMu.Unlock()
		return
	}
	h.unindexServerLocked(serverID, client)
	becameOffline := h.decrementPresence(serverID, client.UserID())
	h.clientsMu.Unlock()
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
	unlock := h.presenceBroadcasts.Lock(userID)
	currentStatus := func() PresenceStatus {
		h.presenceMu.RLock()
		defer h.presenceMu.RUnlock()
		if h.presence[serverID][userID] > 0 {
			return PresenceOnline
		}
		return PresenceOffline
	}
	if currentStatus() != status {
		unlock()
		return
	}
	lastSeenAt := time.Now().Unix()
	h.clientsMu.RLock()
	save := h.lastSeenSaver
	h.clientsMu.RUnlock()
	if save != nil {
		stamp := presenceStamp{lastSeenAt, status}
		h.lastSeenMu.Lock()
		previous, exists := h.lastSeen[userID]
		changed := !exists || previous != stamp
		if changed {
			h.lastSeen[userID] = stamp
		}
		h.lastSeenMu.Unlock()
		if changed {
			save(userID, lastSeenAt)
		}
	}
	event := OutgoingEvent{
		Type: EventPresenceUpdated,
		Data: PresenceUpdatedData{
			ServerID:   serverID,
			UserID:     userID,
			Status:     status,
			LastSeenAt: &lastSeenAt,
		},
	}
	// Persistence can overlap a quick reconnect. Serialize emission and recheck
	// the current state so a delayed offline event cannot hide an online user.
	var failed []*Client
	if currentStatus() == status {
		for _, client := range h.clientsForServer(serverID) {
			if !client.enqueue(event) {
				failed = append(failed, client)
			}
		}
	}
	unlock()
	// Queue cleanup may itself emit presence; never recurse under the key lock.
	for _, client := range failed {
		h.Unregister(client)
	}
}

// SetLastSeenSaver persists only presence transitions, never individual media
// events or heartbeats. The callback runs without any hub lock held.
func (h *Hub) SetLastSeenSaver(save func(userID, at int64)) {
	h.clientsMu.Lock()
	h.lastSeenSaver = save
	h.clientsMu.Unlock()
}

func (h *Hub) broadcastToClients(
	clients []*Client,
	event OutgoingEvent,
) int {
	event = event.shareEncoding()
	delivered := make(
		map[*Client]struct{},
		len(clients),
	)
	deliveredCount := 0
	var failed []*Client

	for _, client := range clients {
		if _, exists := delivered[client]; exists {
			continue
		}

		delivered[client] = struct{}{}

		if !client.enqueue(event) {
			failed = append(failed, client)
			continue
		}

		deliveredCount++
	}

	for _, client := range failed {
		h.Unregister(client)
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

	// A room snapshot is already unique; keep the ordinary publish path free
	// of the deduplication map used by broadcasts to merged recipient lists.
	event = event.shareEncoding()
	delivered := 0
	var failed []*Client
	for _, client := range room.snapshot() {
		if client.enqueue(event) {
			delivered++
		} else {
			failed = append(failed, client)
		}
	}
	for _, client := range failed {
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
	h.channelAccessMu.Lock()
	defer h.channelAccessMu.Unlock()

	h.roomsMu.Lock()
	room := h.rooms[channelID]
	if room == nil {
		h.roomsMu.Unlock()
		h.stopStreamForChannel(
			channelID,
			"voice channel was deleted",
		)
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

	h.stopStreamForChannel(
		channelID,
		"voice channel was deleted",
	)

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

func (h *Hub) Stats() map[string]int {
	h.clientsMu.RLock()
	clients := clientSetSnapshot(h.clients)
	h.clientsMu.RUnlock()
	result := map[string]int{"clients": len(clients), "queue_overflow_total": int(websocketQueueFullTotal.Load())}
	for _, client := range clients {
		stats := client.QueueStats()
		result["queued_normal"] += stats.NormalPending
		result["queued_media"] += stats.MediaPending
		result["queue_high_watermark"] = max(result["queue_high_watermark"], stats.HighWatermark)
	}
	return result
}

// Preserve online/offline transitions in the same second while deduplicating
// identical transitions emitted for several server memberships.
type presenceStamp struct {
	at     int64
	status PresenceStatus
}
