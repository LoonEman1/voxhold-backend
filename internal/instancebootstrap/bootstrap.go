package instancebootstrap

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

type Config struct {
	Username     string
	Password     string
	InstanceName string
}

type Result struct {
	Created           bool
	Username          string
	Password          string
	PasswordGenerated bool
}

func Ensure(ctx context.Context, db *sql.DB, config Config) (Result, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, fmt.Errorf("begin instance bootstrap: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var serverCount int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM servers").Scan(&serverCount); err != nil {
		return Result{}, fmt.Errorf("count instance servers: %w", err)
	}
	if serverCount > 1 {
		return Result{}, fmt.Errorf("invalid instance: expected one server, found %d", serverCount)
	}
	if serverCount == 1 {
		if err := validateOwner(ctx, tx); err != nil {
			return Result{}, err
		}
		if err := tx.Commit(); err != nil {
			return Result{}, fmt.Errorf("commit bootstrap validation: %w", err)
		}
		return Result{}, nil
	}

	var userCount int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&userCount); err != nil {
		return Result{}, fmt.Errorf("count bootstrap users: %w", err)
	}
	if userCount != 0 {
		return Result{}, fmt.Errorf("cannot bootstrap an empty instance with %d existing users", userCount)
	}

	config.Username = strings.TrimSpace(config.Username)
	config.InstanceName = strings.TrimSpace(config.InstanceName)
	if config.Username == "" {
		config.Username = "owner"
	}
	if config.InstanceName == "" {
		config.InstanceName = "Voxhold"
	}
	if length := utf8.RuneCountInString(config.Username); length < 3 || length > 32 {
		return Result{}, fmt.Errorf("bootstrap username must contain from 3 to 32 characters")
	}
	if length := utf8.RuneCountInString(config.InstanceName); length < 1 || length > 64 {
		return Result{}, fmt.Errorf("instance name must contain from 1 to 64 characters")
	}

	generated := false
	if config.Password == "" {
		config.Password, err = generatePassword()
		if err != nil {
			return Result{}, err
		}
		generated = true
	}
	if len(config.Password) < 8 || len([]byte(config.Password)) > 72 {
		return Result{}, fmt.Errorf("bootstrap password must contain from 8 to 72 bytes")
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(config.Password), bcrypt.DefaultCost)
	if err != nil {
		return Result{}, fmt.Errorf("hash bootstrap password: %w", err)
	}

	var userID int64
	if err := tx.QueryRowContext(
		ctx,
		"INSERT INTO users (username, password_hash) VALUES (?, ?) RETURNING id",
		config.Username,
		string(passwordHash),
	).Scan(&userID); err != nil {
		return Result{}, fmt.Errorf("create instance owner: %w", err)
	}

	var serverID int64
	if err := tx.QueryRowContext(
		ctx,
		"INSERT INTO servers (name, created_by) VALUES (?, ?) RETURNING id",
		config.InstanceName,
		userID,
	).Scan(&serverID); err != nil {
		return Result{}, fmt.Errorf("create instance server: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		"INSERT INTO server_members (server_id, user_id, role) VALUES (?, ?, 'owner')",
		serverID,
		userID,
	); err != nil {
		return Result{}, fmt.Errorf("add instance owner membership: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Result{}, fmt.Errorf("commit instance bootstrap: %w", err)
	}

	return Result{
		Created:           true,
		Username:          config.Username,
		Password:          config.Password,
		PasswordGenerated: generated,
	}, nil
}

func Validate(ctx context.Context, db *sql.DB) error {
	var serverCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM servers").Scan(&serverCount); err != nil {
		return fmt.Errorf("count instance servers: %w", err)
	}
	if serverCount != 1 {
		return fmt.Errorf("instance is not initialized: expected one server, found %d", serverCount)
	}
	return validateOwner(ctx, db)
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func validateOwner(ctx context.Context, query rowQuerier) error {
	const statement = `
	SELECT COUNT(*)
	FROM server_members
	JOIN servers ON servers.id = server_members.server_id
	JOIN users ON users.id = server_members.user_id
	WHERE server_members.role = 'owner'
	  AND users.deleted_at IS NULL
	  AND NOT EXISTS (
		SELECT 1 FROM user_bans WHERE user_id = users.id
	  )
	`

	var ownerCount int
	if err := query.QueryRowContext(ctx, statement).Scan(&ownerCount); err != nil {
		return fmt.Errorf("count active instance owners: %w", err)
	}
	if ownerCount != 1 {
		return fmt.Errorf("invalid instance: expected one active owner, found %d", ownerCount)
	}
	return nil
}

func generatePassword() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate bootstrap password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
