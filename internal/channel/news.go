package channel

import (
	"context"
	"slices"
)

type NewsSettings struct {
	PublisherIDs    []int64 `json:"publisher_ids"`
	CommentsEnabled bool    `json:"comments_enabled"`
}

type NewsRepository interface {
	GetNews(context.Context, int64, int64, int64) (NewsSettings, error)
	SetNews(context.Context, int64, int64, int64, NewsSettings) (NewsSettings, error)
}

func (s *Service) GetNews(ctx context.Context, serverID, channelID, userID int64) (NewsSettings, error) {
	repo, ok := s.repository.(NewsRepository)
	if !ok {
		return NewsSettings{}, ErrAccessInvalid
	}
	return repo.GetNews(ctx, serverID, channelID, userID)
}

func (s *Service) SetNews(ctx context.Context, serverID, channelID, userID int64, input NewsSettings) (NewsSettings, error) {
	if len(input.PublisherIDs) > 1000 {
		return NewsSettings{}, ErrAccessInvalid
	}
	for _, id := range input.PublisherIDs {
		if id <= 0 {
			return NewsSettings{}, ErrAccessInvalid
		}
	}
	input.PublisherIDs = slices.Clone(input.PublisherIDs)
	slices.Sort(input.PublisherIDs)
	input.PublisherIDs = slices.Compact(input.PublisherIDs)
	repo, ok := s.repository.(NewsRepository)
	if !ok {
		return NewsSettings{}, ErrAccessInvalid
	}
	value, err := repo.SetNews(ctx, serverID, channelID, userID, input)
	if err != nil {
		return NewsSettings{}, err
	}
	if publisher, ok := s.events.(interface{ PublishChannelAccessChanged(int64, int64) }); ok {
		publisher.PublishChannelAccessChanged(serverID, channelID)
	}
	return value, nil
}
