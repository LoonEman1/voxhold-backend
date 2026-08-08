package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"voxhold-backend/internal/profile"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		db: db,
	}
}

func (r *Repository) GetByUserID(
	ctx context.Context,
	userID int64,
) (profile.Profile, error) {
	return getByUserID(
		ctx,
		r.db,
		userID,
	)
}

func (r *Repository) Update(
	ctx context.Context,
	userID int64,
	about *string,
	countryCode *string,
) (profile.Profile, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return profile.Profile{}, fmt.Errorf(
			"begin update profile transaction: %w",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	aboutProvided := 0
	aboutValue := ""

	if about != nil {
		aboutProvided = 1
		aboutValue = *about
	}

	countryCodeProvided := 0
	countryCodeValue := ""

	if countryCode != nil {
		countryCodeProvided = 1
		countryCodeValue = *countryCode
	}

	const updateQuery = `
	INSERT INTO user_profiles (
		user_id,
		about,
		country_code,
		updated_at
	)
	VALUES (
		?,
		CASE WHEN ? = 1 THEN ? ELSE '' END,
		CASE
			WHEN ? = 1 THEN NULLIF(?, '')
			ELSE NULL
		END,
		unixepoch()
	)
	ON CONFLICT(user_id) DO UPDATE SET
		about = CASE
			WHEN ? = 1 THEN excluded.about
			ELSE user_profiles.about
		END,
		country_code = CASE
			WHEN ? = 1 THEN excluded.country_code
			ELSE user_profiles.country_code
		END,
		updated_at = unixepoch()
	`

	_, err = tx.ExecContext(
		ctx,
		updateQuery,
		userID,
		aboutProvided,
		aboutValue,
		countryCodeProvided,
		countryCodeValue,
		aboutProvided,
		countryCodeProvided,
	)
	if err != nil {
		return profile.Profile{}, fmt.Errorf(
			"upsert user profile: %w",
			err,
		)
	}

	updatedProfile, err := getByUserID(
		ctx,
		tx,
		userID,
	)
	if err != nil {
		return profile.Profile{}, fmt.Errorf(
			"get updated profile: %w",
			err,
		)
	}

	if err := tx.Commit(); err != nil {
		return profile.Profile{}, fmt.Errorf(
			"commit update profile transaction: %w",
			err,
		)
	}

	return updatedProfile, nil
}

type rowQuerier interface {
	QueryRowContext(
		ctx context.Context,
		query string,
		args ...any,
	) *sql.Row
}

func getByUserID(
	ctx context.Context,
	queryer rowQuerier,
	userID int64,
) (profile.Profile, error) {
	const getQuery = `
	SELECT
		users.id,
		users.username,
		COALESCE(user_profiles.about, ''),
		user_profiles.country_code,
		users.created_at,
		user_profiles.last_seen_at,
		user_profiles.updated_at
	FROM users
	LEFT JOIN user_profiles
		ON user_profiles.user_id = users.id
	WHERE users.id = ?
	`

	var foundProfile profile.Profile

	err := queryer.QueryRowContext(
		ctx,
		getQuery,
		userID,
	).Scan(
		&foundProfile.UserID,
		&foundProfile.Username,
		&foundProfile.About,
		&foundProfile.CountryCode,
		&foundProfile.CreatedAt,
		&foundProfile.LastSeenAt,
		&foundProfile.UpdatedAt,
	)
	if err != nil {
		return profile.Profile{}, fmt.Errorf(
			"select user profile: %w",
			err,
		)
	}

	return foundProfile, nil
}

func (r *Repository) GetVisibleByUserID(
	ctx context.Context,
	requesterUserID int64,
	targetUserID int64,
) (profile.Profile, error) {
	const getVisibleQuery = `
	SELECT
		target_user.id,
		target_user.username,
		COALESCE(target_profile.about, ''),
		target_profile.country_code,
		target_user.created_at,
		target_profile.last_seen_at,
		target_profile.updated_at
	FROM users AS target_user
	LEFT JOIN user_profiles AS target_profile
		ON target_profile.user_id = target_user.id
	WHERE target_user.id = ?
		AND (
			target_user.id = ?
			OR EXISTS (
				SELECT 1
				FROM server_members AS requester_membership
				JOIN server_members AS target_membership
					ON target_membership.server_id =
						requester_membership.server_id
				WHERE requester_membership.user_id = ?
					AND target_membership.user_id =
						target_user.id
			)
		)
	`

	var foundProfile profile.Profile

	err := r.db.QueryRowContext(
		ctx,
		getVisibleQuery,
		targetUserID,
		requesterUserID,
		requesterUserID,
	).Scan(
		&foundProfile.UserID,
		&foundProfile.Username,
		&foundProfile.About,
		&foundProfile.CountryCode,
		&foundProfile.CreatedAt,
		&foundProfile.LastSeenAt,
		&foundProfile.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return profile.Profile{},
				profile.ErrProfileNotFound
		}

		return profile.Profile{}, fmt.Errorf(
			"select visible user profile: %w",
			err,
		)
	}

	return foundProfile, nil
}
