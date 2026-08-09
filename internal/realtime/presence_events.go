package realtime

const (
	EventPresenceSnapshot EventType = "presence.snapshot"
	EventPresenceUpdated  EventType = "presence.updated"
)

type PresenceStatus string

const (
	PresenceOnline  PresenceStatus = "online"
	PresenceOffline PresenceStatus = "offline"
)

type ServerPresenceSnapshotData struct {
	ServerID      int64   `json:"server_id"`
	OnlineUserIDs []int64 `json:"online_user_ids"`
}

type PresenceSnapshotData struct {
	Servers []ServerPresenceSnapshotData `json:"servers"`
}

type PresenceUpdatedData struct {
	ServerID int64          `json:"server_id"`
	UserID   int64          `json:"user_id"`
	Status   PresenceStatus `json:"status"`
}
