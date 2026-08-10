package realtime

import "voxhold-backend/internal/readstate"

var _ readstate.EventPublisher = (*ReadEventPublisher)(nil)

type ReadEventPublisher struct {
	hub *Hub
}

func NewReadEventPublisher(
	hub *Hub,
) *ReadEventPublisher {
	return &ReadEventPublisher{hub: hub}
}

func (p *ReadEventPublisher) PublishChannelRead(
	read readstate.ChannelRead,
) {
	p.hub.PublishToChannelAndUser(
		read.ChannelID,
		read.UserID,
		OutgoingEvent{
			Type: EventChannelRead,
			Data: newChannelReadData(read),
		},
	)
}

func NewReadSnapshotData(
	reads []readstate.ChannelRead,
) ReadSnapshotData {
	return ReadSnapshotData{
		Reads: newChannelReadDataSlice(reads),
	}
}

func NewChannelReadSnapshotData(
	serverID int64,
	channelID int64,
	reads []readstate.ChannelRead,
) ChannelReadSnapshotData {
	return ChannelReadSnapshotData{
		ServerID:  serverID,
		ChannelID: channelID,
		Reads:     newChannelReadDataSlice(reads),
	}
}

func newChannelReadDataSlice(
	reads []readstate.ChannelRead,
) []ChannelReadData {
	values := make(
		[]ChannelReadData,
		0,
		len(reads),
	)

	for _, read := range reads {
		values = append(values, newChannelReadData(read))
	}

	return values
}

func newChannelReadData(
	read readstate.ChannelRead,
) ChannelReadData {
	return ChannelReadData{
		ServerID:          read.ServerID,
		ChannelID:         read.ChannelID,
		UserID:            read.UserID,
		LastReadMessageID: read.LastReadMessageID,
		UpdatedAt:         read.UpdatedAt,
	}
}
