package messagehttp

import "voxhold-backend/internal/message"

type searchResultResponse struct {
	ID          int64          `json:"id"`
	ChannelID   int64          `json:"channel_id"`
	ChannelName string         `json:"channel_name"`
	Author      authorResponse `json:"author"`
	Content     string         `json:"content"`
	CreatedAt   int64          `json:"created_at"`
	EditedAt    *int64         `json:"edited_at"`
}

type searchPageResponse struct {
	Messages   []searchResultResponse `json:"messages"`
	Pagination paginationResponse     `json:"pagination"`
}

func newSearchPageResponse(
	page message.SearchPage,
) searchPageResponse {
	messages := make(
		[]searchResultResponse,
		0,
		len(page.Results),
	)

	for _, result := range page.Results {
		messages = append(
			messages,
			searchResultResponse{
				ID:          result.Message.ID,
				ChannelID:   result.Message.ChannelID,
				ChannelName: result.ChannelName,
				Author: authorResponse{
					UserID:   result.Message.Author.UserID,
					Username: result.Message.Author.Username,
				},
				Content:   result.Message.Content,
				CreatedAt: result.Message.CreatedAt,
				EditedAt:  result.Message.EditedAt,
			},
		)
	}

	return searchPageResponse{
		Messages: messages,
		Pagination: paginationResponse{
			NextBeforeID: page.NextBeforeID,
			HasMore:      page.HasMore,
		},
	}
}

type messageContextResponse struct {
	Messages        []messageResponse `json:"messages"`
	TargetMessageID int64             `json:"target_message_id"`
	TargetIndex     int               `json:"target_index"`
}

func newMessageContextResponse(
	value message.Context,
) messageContextResponse {
	messages := make(
		[]messageResponse,
		0,
		len(value.Messages),
	)

	for _, contextMessage := range value.Messages {
		messages = append(
			messages,
			newMessageResponse(contextMessage),
		)
	}

	return messageContextResponse{
		Messages:        messages,
		TargetMessageID: value.TargetMessageID,
		TargetIndex:     value.TargetIndex,
	}
}
