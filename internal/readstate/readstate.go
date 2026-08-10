package readstate

import "errors"

var (
	ErrForbidden = errors.New(
		"not allowed to access channel read states",
	)
	ErrChannelNotFound = errors.New(
		"channel not found",
	)
	ErrTextChannelRequired = errors.New(
		"read states are only available for text channels",
	)
	ErrMessageNotFound = errors.New(
		"message not found",
	)
	ErrMessageIDInvalid = errors.New(
		"last_read_message_id must be positive",
	)
)

type ChannelRead struct {
	ServerID          int64
	ChannelID         int64
	UserID            int64
	LastReadMessageID int64
	UpdatedAt         int64
}

type MarkInput struct {
	LastReadMessageID int64
}

func (i MarkInput) Validate() error {
	if i.LastReadMessageID <= 0 {
		return ErrMessageIDInvalid
	}

	return nil
}
