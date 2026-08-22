package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	Retention          = 24 * time.Hour
	MaxBatchEvents     = 64
	MaxDetailsBytes    = 4 * 1024
	MaxEventsPerUser   = 10_000
	MaxStoredEvents    = 50_000
	DefaultListLimit   = 1_000
	MaxListLimit       = 5_000
	MaxClientClockSkew = 10 * time.Minute
	MaxClientEventAge  = 48 * time.Hour
)

var (
	ErrInvalidBatch = errors.New("invalid client diagnostic batch")
	ErrInvalidEvent = errors.New("invalid client diagnostic event")
	ErrForbidden    = errors.New("client diagnostics access is forbidden")
)

type EventInput struct {
	ClientTimestampMS int64
	Category          string
	Name              string
	Level             string
	DetailsJSON       string
}

type BatchInput struct {
	SessionID     string
	ClientVersion string
	Platform      string
	Events        []EventInput
}

type Event struct {
	ID                int64
	CreatedAt         int64
	ClientTimestampMS int64
	UserID            int64
	Username          string
	SessionID         string
	ClientVersion     string
	Platform          string
	Category          string
	Name              string
	Level             string
	Details           json.RawMessage
}

type ListFilter struct {
	Since     int64
	SessionID string
	Category  string
	Limit     int
}

type Repository interface {
	InsertBatch(
		ctx context.Context,
		userID int64,
		input BatchInput,
	) (int, error)
	List(
		ctx context.Context,
		filter ListFilter,
	) ([]Event, error)
	IsOwner(ctx context.Context, userID int64) (bool, error)
}

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now}
}

func (s *Service) Ingest(
	ctx context.Context,
	userID int64,
	input BatchInput,
) (int, error) {
	if userID <= 0 || s.repository == nil {
		return 0, ErrInvalidBatch
	}

	input.SessionID = strings.TrimSpace(input.SessionID)
	input.ClientVersion = strings.TrimSpace(input.ClientVersion)
	input.Platform = truncateString(strings.TrimSpace(input.Platform), 200)
	if !validIdentifier(input.SessionID, 64) ||
		len(input.ClientVersion) > 32 ||
		len(input.Events) == 0 ||
		len(input.Events) > MaxBatchEvents {

		return 0, ErrInvalidBatch
	}

	now := s.now()
	minimumTimestamp := now.Add(-MaxClientEventAge).UnixMilli()
	maximumTimestamp := now.Add(MaxClientClockSkew).UnixMilli()
	for index := range input.Events {
		event := &input.Events[index]
		event.Category = strings.TrimSpace(event.Category)
		event.Name = strings.TrimSpace(event.Name)
		event.Level = strings.TrimSpace(event.Level)
		if !validIdentifier(event.Category, 32) ||
			!validEventName(event.Name) ||
			!validLevel(event.Level) ||
			event.ClientTimestampMS < minimumTimestamp ||
			event.ClientTimestampMS > maximumTimestamp {

			return 0, ErrInvalidEvent
		}
		cleaned, err := sanitizeDetailsJSON(event.DetailsJSON)
		if err != nil {
			return 0, ErrInvalidEvent
		}
		event.DetailsJSON = cleaned
	}

	return s.repository.InsertBatch(ctx, userID, input)
}

func (s *Service) List(
	ctx context.Context,
	userID int64,
	filter ListFilter,
) ([]Event, error) {
	if userID <= 0 || s.repository == nil {
		return nil, ErrForbidden
	}
	owner, err := s.repository.IsOwner(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !owner {
		return nil, ErrForbidden
	}

	now := s.now().Unix()
	minimum := now - int64(Retention/time.Second)
	if filter.Since == 0 || filter.Since < minimum {
		filter.Since = minimum
	}
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	filter.Category = strings.TrimSpace(filter.Category)
	if filter.SessionID != "" && !validIdentifier(filter.SessionID, 64) {
		return nil, ErrInvalidBatch
	}
	if filter.Category != "" && !validIdentifier(filter.Category, 32) {
		return nil, ErrInvalidBatch
	}
	if filter.Limit == 0 {
		filter.Limit = DefaultListLimit
	}
	if filter.Limit < 1 || filter.Limit > MaxListLimit {
		return nil, ErrInvalidBatch
	}

	return s.repository.List(ctx, filter)
}

func validIdentifier(value string, maximum int) bool {
	if value == "" || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("._:-", character) {
			continue
		}
		return false
	}
	return true
}

func validEventName(value string) bool {
	return value != "" && len(value) <= 80 && !strings.ContainsAny(value, "\r\n\t")
}

func validLevel(value string) bool {
	switch value {
	case "debug", "info", "warn", "error":
		return true
	default:
		return false
	}
}
