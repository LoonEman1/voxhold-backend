package messagehttp

import "voxhold-backend/internal/message"

type authorResponse struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
}

type messageResponse struct {
	Poll              *message.Poll             `json:"poll,omitempty"`
	Attachments       []message.Attachment      `json:"attachments,omitempty"`
	ThreadRootID      *int64                    `json:"thread_root_id,omitempty"`
	CommentCount      int64                     `json:"comment_count"`
	ClientMessageID   string                    `json:"client_message_id,omitempty"`
	ID                int64                     `json:"id"`
	ChannelID         int64                     `json:"channel_id"`
	Author            authorResponse            `json:"author"`
	Content           string                    `json:"content"`
	CreatedAt         int64                     `json:"created_at"`
	EditedAt          *int64                    `json:"edited_at"`
	Revision          int64                     `json:"revision"`
	Reply             *message.ReplyReference   `json:"reply,omitempty"`
	Forward           *message.ForwardReference `json:"forward,omitempty"`
	Reactions         []message.Reaction        `json:"reactions"`
	ReactionsRevision int64                     `json:"reactions_revision"`
}

func newMessageResponse(
	value message.Message,
) messageResponse {
	return messageResponse{
		Poll:            value.Poll,
		Attachments:     value.Attachments,
		ClientMessageID: value.ClientMessageID,
		ThreadRootID:    value.ThreadRootID, CommentCount: value.CommentCount,
		ID:        value.ID,
		ChannelID: value.ChannelID,
		Author: authorResponse{
			UserID:   value.Author.UserID,
			Username: value.Author.Username,
		},
		Content:   value.Content,
		CreatedAt: value.CreatedAt,
		EditedAt:  value.EditedAt,
		Revision:  value.Revision,
		Reply:     value.Reply, Forward: value.Forward, Reactions: value.Reactions, ReactionsRevision: value.ReactionsRevision,
	}
}

type paginationResponse struct {
	NextBeforeID *int64 `json:"next_before_id"`
	HasMore      bool   `json:"has_more"`
}

type messagePageResponse struct {
	Messages   []messageResponse  `json:"messages"`
	Pagination paginationResponse `json:"pagination"`
}

type pinnedMessageResponse struct {
	Message  messageResponse `json:"message"`
	PinnedBy authorResponse  `json:"pinned_by"`
	PinnedAt int64           `json:"pinned_at"`
}

func newPinnedMessageResponse(
	value message.PinnedMessage,
) pinnedMessageResponse {
	return pinnedMessageResponse{
		Message: newMessageResponse(value.Message),
		PinnedBy: authorResponse{
			UserID:   value.PinnedBy.UserID,
			Username: value.PinnedBy.Username,
		},
		PinnedAt: value.PinnedAt,
	}
}

func newPinnedMessagesResponse(
	values []message.PinnedMessage,
) []pinnedMessageResponse {
	response := make(
		[]pinnedMessageResponse,
		0,
		len(values),
	)

	for _, value := range values {
		response = append(
			response,
			newPinnedMessageResponse(value),
		)
	}

	return response
}

func newMessagePageResponse(
	page message.Page,
) messagePageResponse {
	messages := make(
		[]messageResponse,
		0,
		len(page.Messages),
	)

	for _, value := range page.Messages {
		messages = append(
			messages,
			newMessageResponse(value),
		)
	}

	return messagePageResponse{
		Messages: messages,
		Pagination: paginationResponse{
			NextBeforeID: page.NextBeforeID,
			HasMore:      page.HasMore,
		},
	}
}
