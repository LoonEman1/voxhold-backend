package readstate

import (
	"context"
	"testing"
)

type repositoryStub struct {
	read    ChannelRead
	changed bool
	err     error
}

func (r *repositoryStub) Mark(
	context.Context,
	int64,
	int64,
	int64,
	int64,
) (ChannelRead, bool, error) {
	return r.read, r.changed, r.err
}

func (r *repositoryStub) ListByUserID(
	context.Context,
	int64,
) ([]ChannelRead, error) {
	return nil, nil
}

func (r *repositoryStub) ListByChannelID(
	context.Context,
	int64,
	int64,
	int64,
) ([]ChannelRead, error) {
	return nil, nil
}

type eventPublisherStub struct {
	published []ChannelRead
}

func (p *eventPublisherStub) PublishChannelRead(
	read ChannelRead,
) {
	p.published = append(p.published, read)
}

func TestMarkPublishesOnlyAdvancedReadState(t *testing.T) {
	read := ChannelRead{
		ServerID:          1,
		ChannelID:         2,
		UserID:            3,
		LastReadMessageID: 4,
	}

	for _, test := range []struct {
		name              string
		changed           bool
		expectedPublished int
	}{
		{
			name:              "advanced cursor",
			changed:           true,
			expectedPublished: 1,
		},
		{
			name:              "unchanged cursor",
			changed:           false,
			expectedPublished: 0,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &repositoryStub{
				read:    read,
				changed: test.changed,
			}
			events := &eventPublisherStub{}
			service := NewService(repository, events)

			result, err := service.Mark(
				context.Background(),
				1,
				2,
				3,
				MarkInput{LastReadMessageID: 4},
			)
			if err != nil {
				t.Fatalf("mark read state: %v", err)
			}
			if result != read {
				t.Fatalf("unexpected read state: %+v", result)
			}
			if len(events.published) != test.expectedPublished {
				t.Fatalf(
					"unexpected published events: %d",
					len(events.published),
				)
			}
		})
	}
}
