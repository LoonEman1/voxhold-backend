// Package adminreset implements operator-only account recovery.
package adminreset

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"

	"golang.org/x/crypto/bcrypt"
)

const (
	passwordLength   = 20
	passwordAlphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
)

var ErrUserNotFound = errors.New("user not found or deleted")

// ResetPassword sets a random password, drops every session of the user and
// returns the new password. It is shown once and never stored in plain text.
func ResetPassword(ctx context.Context, db *sql.DB, username string) (string, error) {
	password, err := generatePassword()
	if err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin reset: %w", err)
	}
	defer tx.Rollback()
	var userID int64
	err = tx.QueryRowContext(ctx,
		`UPDATE users SET password_hash=? WHERE username=? AND deleted_at IS NULL RETURNING id`,
		string(hash), username).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrUserNotFound
	}
	if err != nil {
		return "", fmt.Errorf("update password: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
		return "", fmt.Errorf("delete sessions: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return "", fmt.Errorf("commit reset: %w", err)
	}
	return password, nil
}

func generatePassword() (string, error) {
	out := make([]byte, passwordLength)
	limit := big.NewInt(int64(len(passwordAlphabet)))
	for i := range out {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("generate password: %w", err)
		}
		out[i] = passwordAlphabet[n.Int64()]
	}
	return string(out), nil
}
