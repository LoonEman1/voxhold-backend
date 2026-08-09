package message

type Author struct {
	UserID   int64
	Username string
}

type Message struct {
	ID        int64
	ChannelID int64
	Author    Author
	Content   string
	CreatedAt int64
	EditedAt  *int64
}

type Page struct {
	Messages     []Message
	NextBeforeID *int64
	HasMore      bool
}
