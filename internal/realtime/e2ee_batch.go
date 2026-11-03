package realtime

const EventE2EEKeys EventType = "e2ee.keys"

// Batching changes only the transport; recipients still receive e2ee.key.
const MaxE2EEBatchRecipients = 32

type E2EEKeyRecipient struct {
	UserID   int64  `json:"recipient_user_id"`
	DeviceID string `json:"recipient_device_id"`
	Payload  string `json:"payload"`
}

type E2EEKeyBatchData struct {
	ChannelID int64              `json:"channel_id"`
	Keys      []E2EEKeyRecipient `json:"keys"`
}
