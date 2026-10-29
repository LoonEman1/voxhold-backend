package message

type Author struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
}

type Message struct {
	Poll        *Poll `json:"poll,omitempty"`
	Attachments []Attachment
	// Replayed send keys acknowledge the original message without alerting again.
	Replayed          bool
	ClientMessageID   string
	ID                int64
	ChannelID         int64
	Author            Author
	Content           string
	CreatedAt         int64
	EditedAt          *int64
	Revision          int64
	Reply             *ReplyReference
	Forward           *ForwardReference
	Reactions         []Reaction
	ReactionsRevision int64
	ThreadRootID      *int64
	CommentCount      int64
}

type Page struct {
	Messages     []Message
	NextBeforeID *int64
	HasMore      bool
}
