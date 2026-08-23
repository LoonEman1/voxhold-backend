package message

import (
	"context"
	"fmt"
	"slices"
)

type Service struct {
	repository Repository
	events     EventPublisher
}

func NewService(
	repository Repository,
	events EventPublisher,
) *Service {
	return &Service{
		repository: repository,
		events:     events,
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

	s.events.PublishMessageCreated(
		createdMessage,
	)

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

func (s *Service) Update(
	ctx context.Context,
	serverID int64,
	channelID int64,
	messageID int64,
	userID int64,
	input UpdateInput,
) (Message, error) {
	if serverID <= 0 || channelID <= 0 || messageID <= 0 {
		return Message{}, ErrMessageNotFound
	}

	if userID <= 0 {
		return Message{}, ErrForbidden
	}

	input = input.Normalize()
	if err := input.Validate(); err != nil {
		return Message{}, err
	}

	updatedMessage, err := s.repository.Update(
		ctx,
		serverID,
		channelID,
		messageID,
		userID,
		input.Content,
	)
	if err != nil {
		return Message{}, fmt.Errorf(
			"update message: %w",
			err,
		)
	}

	s.events.PublishMessageUpdated(updatedMessage)

	return updatedMessage, nil
}

func (s *Service) Delete(
	ctx context.Context,
	serverID int64,
	channelID int64,
	messageID int64,
	userID int64,
) error {
	if serverID <= 0 || channelID <= 0 || messageID <= 0 {
		return ErrMessageNotFound
	}

	if userID <= 0 {
		return ErrForbidden
	}

	deletedMessage, err := s.repository.Delete(
		ctx,
		serverID,
		channelID,
		messageID,
		userID,
	)
	if err != nil {
		return fmt.Errorf("delete message: %w", err)
	}

	s.events.PublishMessageDeleted(deletedMessage)

	return nil
}

func (s *Service) Pin(
	ctx context.Context,
	serverID int64,
	channelID int64,
	messageID int64,
	userID int64,
) (PinnedMessage, error) {
	if serverID <= 0 || channelID <= 0 || messageID <= 0 {
		return PinnedMessage{}, ErrMessageNotFound
	}

	pinnedMessage, created, err := s.repository.Pin(
		ctx,
		serverID,
		channelID,
		messageID,
		userID,
	)
	if err != nil {
		return PinnedMessage{}, fmt.Errorf("pin message: %w", err)
	}

	if created {
		s.events.PublishMessagePinned(pinnedMessage)
	}

	return pinnedMessage, nil
}

func (s *Service) Unpin(
	ctx context.Context,
	serverID int64,
	channelID int64,
	messageID int64,
	userID int64,
) error {
	if serverID <= 0 || channelID <= 0 || messageID <= 0 {
		return ErrMessageNotFound
	}

	deleted, err := s.repository.Unpin(
		ctx,
		serverID,
		channelID,
		messageID,
		userID,
	)
	if err != nil {
		return fmt.Errorf("unpin message: %w", err)
	}

	if deleted {
		s.events.PublishMessageUnpinned(
			channelID,
			messageID,
		)
	}

	return nil
}

func (s *Service) ListPinned(
	ctx context.Context,
	serverID int64,
	channelID int64,
	userID int64,
) ([]PinnedMessage, error) {
	if serverID <= 0 || channelID <= 0 {
		return nil, ErrChannelNotFound
	}

	if userID <= 0 {
		return nil, ErrForbidden
	}

	pinnedMessages, err := s.repository.ListPinned(
		ctx,
		serverID,
		channelID,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list pinned messages: %w",
			err,
		)
	}

	if pinnedMessages == nil {
		pinnedMessages = make([]PinnedMessage, 0)
	}

	return pinnedMessages, nil
}
