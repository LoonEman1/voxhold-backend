package account

import (
	"context"
	"database/sql"
	"fmt"
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{
		db: db,
	}
}

func (r *UserRepository) Create(
	ctx context.Context,
	username string,
	passwordHash string,
) (User, error) {
	const query = `
	INSERT INTO users (username, password_hash)
	VALUES (?, ?)
	RETURNING id, username, created_at, password_hash
	`

	var user User
	err := r.db.QueryRowContext(
		ctx,
		query,
		username,
		passwordHash,
	).Scan(&user.ID, &user.Username, &user.CreatedAt, &passwordHash)
	if err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

func (r *UserRepository) FindByUsername(
	ctx context.Context,
	username string,
) (User, error) {
	const query = `
	SELECT id, username, created_at
	FROM USERS
	WHERE username = ?
	`

	var user User

	err := r.db.QueryRowContext(
		ctx,
		query,
		username,
	).Scan(&user.ID,
		&user.Username,
		&user.CreatedAt,
	)
	if err != nil {
		return User{}, fmt.Errorf("find user by username: %w", err)
	}

	return user, nil
}
