package message

import (
	"context"
	"fmt"
)

func (s *Service) Search(
	ctx context.Context,
	serverID int64,
	userID int64,
	input SearchInput,
) (SearchPage, error) {
	if serverID <= 0 || userID <= 0 {
		return SearchPage{}, ErrForbidden
	}

	input = input.Normalize()
	if err := input.Validate(); err != nil {
		return SearchPage{}, err
	}

	results, err := s.repository.Search(
		ctx,
		serverID,
		userID,
		input.Query,
		input.BeforeID,
		input.Limit+1,
	)
	if err != nil {
		return SearchPage{}, fmt.Errorf(
			"search messages: %w",
			err,
		)
	}

	hasMore := len(results) > input.Limit
	if hasMore {
		results = results[:input.Limit]
	}

	var nextBeforeID *int64
	if hasMore && len(results) > 0 {
		value := results[len(results)-1].Message.ID
		nextBeforeID = &value
	}

	if results == nil {
		results = make([]SearchResult, 0)
	}

	return SearchPage{
		Results:      results,
		NextBeforeID: nextBeforeID,
		HasMore:      hasMore,
	}, nil
}

func (s *Service) GetContext(
	ctx context.Context,
	serverID int64,
	channelID int64,
	messageID int64,
	userID int64,
	input ContextInput,
) (Context, error) {
	if serverID <= 0 || channelID <= 0 || messageID <= 0 {
		return Context{}, ErrMessageNotFound
	}

	if userID <= 0 {
		return Context{}, ErrForbidden
	}

	input = input.Normalize()
	if err := input.Validate(); err != nil {
		return Context{}, err
	}

	contextMessages, err := s.repository.GetContext(
		ctx,
		serverID,
		channelID,
		messageID,
		userID,
		input.Before,
		input.After,
	)
	if err != nil {
		return Context{}, fmt.Errorf(
			"get message context: %w",
			err,
		)
	}

	return contextMessages, nil
}
