package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"voxhold-backend/internal/diagnostics"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) InsertBatch(
	ctx context.Context,
	userID int64,
	input diagnostics.BatchInput,
) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin diagnostic batch: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	cutoff := time.Now().Add(-diagnostics.Retention).Unix()
	if _, err := tx.ExecContext(
		ctx,
		"DELETE FROM client_diagnostic_events WHERE created_at < ?",
		cutoff,
	); err != nil {
		return 0, fmt.Errorf("prune expired client diagnostics: %w", err)
	}

	var currentCount int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM client_diagnostic_events
		 WHERE user_id = ? AND created_at >= ?`,
		userID,
		cutoff,
	).Scan(&currentCount); err != nil {
		return 0, fmt.Errorf("count user client diagnostics: %w", err)
	}

	remaining := diagnostics.MaxEventsPerUser - currentCount
	if remaining < 0 {
		remaining = 0
	}
	accepted := min(len(input.Events), remaining)
	if accepted > 0 {
		statement, err := tx.PrepareContext(ctx, `
		INSERT INTO client_diagnostic_events (
			client_timestamp_ms,
			user_id,
			session_id,
			client_version,
			platform,
			category,
			event_name,
			level,
			details_json
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return 0, fmt.Errorf("prepare client diagnostic insert: %w", err)
		}
		defer statement.Close()

		for _, event := range input.Events[:accepted] {
			if _, err := statement.ExecContext(
				ctx,
				event.ClientTimestampMS,
				userID,
				input.SessionID,
				input.ClientVersion,
				input.Platform,
				event.Category,
				event.Name,
				event.Level,
				event.DetailsJSON,
			); err != nil {
				return 0, fmt.Errorf("insert client diagnostic: %w", err)
			}
		}
	}

	if _, err := tx.ExecContext(
		ctx,
		`DELETE FROM client_diagnostic_events
		 WHERE id IN (
			SELECT id
			FROM client_diagnostic_events
			ORDER BY created_at DESC, id DESC
			LIMIT -1 OFFSET ?
		 )`,
		diagnostics.MaxStoredEvents,
	); err != nil {
		return 0, fmt.Errorf("enforce client diagnostic row limit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit diagnostic batch: %w", err)
	}
	return accepted, nil
}

func (r *Repository) List(
	ctx context.Context,
	filter diagnostics.ListFilter,
) ([]diagnostics.Event, error) {
	query := strings.Builder{}
	query.WriteString(`
	SELECT
		client_diagnostic_events.id,
		client_diagnostic_events.created_at,
		client_diagnostic_events.client_timestamp_ms,
		client_diagnostic_events.user_id,
		users.username,
		client_diagnostic_events.session_id,
		client_diagnostic_events.client_version,
		client_diagnostic_events.platform,
		client_diagnostic_events.category,
		client_diagnostic_events.event_name,
		client_diagnostic_events.level,
		client_diagnostic_events.details_json
	FROM client_diagnostic_events
	JOIN users ON users.id = client_diagnostic_events.user_id
	WHERE client_diagnostic_events.created_at >= ?
	`)
	arguments := []any{filter.Since}
	if filter.SessionID != "" {
		query.WriteString(" AND client_diagnostic_events.session_id = ?")
		arguments = append(arguments, filter.SessionID)
	}
	if filter.Category != "" {
		query.WriteString(" AND client_diagnostic_events.category = ?")
		arguments = append(arguments, filter.Category)
	}
	query.WriteString(`
	ORDER BY client_diagnostic_events.created_at DESC,
		client_diagnostic_events.id DESC
	LIMIT ?
	`)
	arguments = append(arguments, filter.Limit)

	rows, err := r.db.QueryContext(ctx, query.String(), arguments...)
	if err != nil {
		return nil, fmt.Errorf("list client diagnostics: %w", err)
	}
	defer rows.Close()

	events := make([]diagnostics.Event, 0)
	for rows.Next() {
		var event diagnostics.Event
		var details string
		if err := rows.Scan(
			&event.ID,
			&event.CreatedAt,
			&event.ClientTimestampMS,
			&event.UserID,
			&event.Username,
			&event.SessionID,
			&event.ClientVersion,
			&event.Platform,
			&event.Category,
			&event.Name,
			&event.Level,
			&details,
		); err != nil {
			return nil, fmt.Errorf("scan client diagnostic: %w", err)
		}
		event.Details = json.RawMessage(details)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate client diagnostics: %w", err)
	}
	return events, nil
}

func (r *Repository) IsOwner(
	ctx context.Context,
	userID int64,
) (bool, error) {
	const query = `
	SELECT EXISTS (
		SELECT 1
		FROM server_members
		JOIN users ON users.id = server_members.user_id
		WHERE server_members.user_id = ?
		  AND server_members.role = 'owner'
		  AND users.deleted_at IS NULL
	)
	`
	var owner bool
	if err := r.db.QueryRowContext(ctx, query, userID).Scan(&owner); err != nil {
		return false, fmt.Errorf("check diagnostics owner access: %w", err)
	}
	return owner, nil
}
