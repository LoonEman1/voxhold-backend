package message

type Attachment struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	MIMEType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	Deleted   bool   `json:"deleted"`
}
