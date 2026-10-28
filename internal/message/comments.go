package message

import (
	"context"
	"slices"
)

func (s *Service) publishCommentRoot(ctx context.Context, serverID, channelID, userID int64, value Message) {
	if value.ThreadRootID == nil || value.Replayed {
		return
	}
	repo, ok := s.repository.(interface {
		GetPost(context.Context, int64, int64, int64, int64) (Message, error)
	})
	if !ok {
		return
	}
	root, err := repo.GetPost(ctx, serverID, channelID, userID, *value.ThreadRootID)
	if err == nil {
		s.events.PublishMessageUpdated(root)
	}
}

func (s *Service) ListComments(ctx context.Context, serverID, channelID, postID, userID int64, input ListInput) (Page, error) {
	if serverID <= 0 || channelID <= 0 || postID <= 0 {
		return Page{}, ErrMessageNotFound
	}
	if userID <= 0 {
		return Page{}, ErrForbidden
	}
	input = input.Normalize()
	if err := input.Validate(); err != nil {
		return Page{}, err
	}
	repo, ok := s.repository.(interface {
		ListComments(context.Context, int64, int64, int64, int64, *int64, int) ([]Message, error)
	})
	if !ok {
		return Page{}, ErrMessageNotFound
	}
	values, err := repo.ListComments(ctx, serverID, channelID, userID, postID, input.BeforeID, input.Limit+1)
	if err != nil {
		return Page{}, err
	}
	page := Page{HasMore: len(values) > input.Limit}
	if page.HasMore {
		values = values[:input.Limit]
	}
	slices.Reverse(values)
	page.Messages = values
	if page.HasMore {
		id := values[0].ID
		page.NextBeforeID = &id
	}
	return page, nil
}
