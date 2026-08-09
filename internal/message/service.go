package message

import (
	"context"
	"fmt"
	"slices"
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{
		repository: repository,
	}
}

func (s *Service) Create(
	ctx context.Context,
	serverID int64,
	channelID int64,
	authorUserID int64,
	input CreateInput,
) (Message, error) {
	if serverID <= 0 || channelID <= 0 {
		return Message{}, ErrChannelNotFound
	}

	if authorUserID <= 0 {
		return Message{}, ErrForbidden
	}

	input = input.Normalize()

	if err := input.Validate(); err != nil {
		return Message{}, err
	}

	createdMessage, err := s.repository.Create(
		ctx,
		serverID,
		channelID,
		authorUserID,
		input.Content,
	)
	if err != nil {
		return Message{}, fmt.Errorf(
			"create message: %w",
			err,
		)
	}

	return createdMessage, nil
}

func (s *Service) ListByChannelID(
	ctx context.Context,
	serverID int64,
	channelID int64,
	userID int64,
	input ListInput,
) (Page, error) {
	if serverID <= 0 || channelID <= 0 {
		return Page{}, ErrChannelNotFound
	}

	if userID <= 0 {
		return Page{}, ErrForbidden
	}

	input = input.Normalize()

	if err := input.Validate(); err != nil {
		return Page{}, err
	}

	messages, err := s.repository.ListByChannelID(
		ctx,
		serverID,
		channelID,
		userID,
		input.BeforeID,
		input.Limit+1,
	)
	if err != nil {
		return Page{}, fmt.Errorf(
			"list channel messages: %w",
			err,
		)
	}

	hasMore := len(messages) > input.Limit

	if hasMore {
		messages = messages[:input.Limit]
	}

	slices.Reverse(messages)

	var nextBeforeID *int64

	if hasMore && len(messages) > 0 {
		value := messages[0].ID
		nextBeforeID = &value
	}

	if messages == nil {
		messages = make([]Message, 0)
	}

	return Page{
		Messages:     messages,
		NextBeforeID: nextBeforeID,
		HasMore:      hasMore,
	}, nil
}
