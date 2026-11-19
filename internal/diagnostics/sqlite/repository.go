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

	for _, event := range input.Events {
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

	var stored int64
	if err := tx.QueryRowContext(ctx, "SELECT total_bytes FROM client_diagnostic_storage WHERE id = 1").Scan(&stored); err != nil {
		return 0, fmt.Errorf("read diagnostic storage size: %w", err)
	}
	if excess := stored - diagnostics.MaxStoredBytes; excess > 0 {
		// Find the smallest oldest-first prefix that frees enough bytes.
		if _, err := tx.ExecContext(ctx, `
			WITH oldest AS (
				SELECT created_at, id, SUM(byte_size) OVER (
					ORDER BY created_at, id ROWS UNBOUNDED PRECEDING
				) AS bytes_to_free
				FROM client_diagnostic_events
			)
			DELETE FROM client_diagnostic_events WHERE (created_at, id) <= (
				SELECT created_at, id FROM oldest WHERE bytes_to_free >= ? LIMIT 1
			)`, excess); err != nil {
			return 0, fmt.Errorf("make diagnostic storage space: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit diagnostic batch: %w", err)
	}
	return len(input.Events), nil
}

// Cleanup keeps each write transaction small and runs outside request handling.
func (r *Repository) Cleanup(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM client_diagnostic_events WHERE id IN (
		SELECT id FROM client_diagnostic_events WHERE created_at < ? ORDER BY created_at, id LIMIT 2000
	)`, time.Now().Add(-diagnostics.Retention).Unix())
	return err
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
	if filter.Before.ID != 0 {
		query.WriteString(" AND (client_diagnostic_events.created_at, client_diagnostic_events.id) < (?, ?)")
		arguments = append(arguments, filter.Before.CreatedAt, filter.Before.ID)
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
