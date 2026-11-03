package account

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrWrongCurrentPassword = errors.New("current password is incorrect")
	ErrSessionNotFound      = errors.New("session not found")
)

type clientMetaKey struct{}

// ClientMeta is stored on new sessions so users can recognise their devices.
type ClientMeta struct {
	UserAgent string
	IP        string
}

func WithClientMeta(ctx context.Context, meta ClientMeta) context.Context {
	if len(meta.UserAgent) > 200 {
		meta.UserAgent = meta.UserAgent[:200]
	}
	return context.WithValue(ctx, clientMetaKey{}, meta)
}

func ClientMetaFromContext(ctx context.Context) ClientMeta {
	meta, _ := ctx.Value(clientMetaKey{}).(ClientMeta)
	return meta
}

// SessionRecord is a stored session; TokenHash never leaves the service.
type SessionRecord struct {
	ID         int64
	TokenHash  []byte
	CreatedAt  int64
	LastUsedAt *int64
	ExpiresAt  int64
	UserAgent  string
	ClientIP   string
}

type SessionView struct {
	ID         int64  `json:"id"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt *int64 `json:"last_used_at"`
	ExpiresAt  int64  `json:"expires_at"`
	UserAgent  string `json:"user_agent"`
	ClientIP   string `json:"client_ip"`
	Current    bool   `json:"current"`
}

type ChangePasswordInput struct {
	CurrentPassword string
	NewPassword     string
	NewConfirm      string
}

func (s *Service) VerifyPassword(ctx context.Context, userID int64, password string) error {
	_, err := s.verifyPassword(ctx, userID, password)
	return err
}

func (s *Service) verifyPassword(ctx context.Context, userID int64, password string) (User, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return User{}, fmt.Errorf("find user: %w", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return User{}, ErrWrongCurrentPassword
	}
	return user, nil
}

// ChangePassword replaces the password and signs out every other session.
func (s *Service) ChangePassword(ctx context.Context, userID int64, currentToken string, input ChangePasswordInput) error {
	if len(input.NewPassword) < 8 {
		return ErrPasswordTooShort
	}
	if len(input.NewPassword) > 72 {
		return ErrPasswordTooLong
	}
	if input.NewPassword != input.NewConfirm {
		return ErrPasswordsDoNotMatch
	}
	user, err := s.verifyPassword(ctx, userID, input.CurrentPassword)
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	hashes, err := s.sessions.ReplacePasswordAndRevokeOthers(ctx, userID, user.PasswordHash, string(hash), hashSessionToken(strings.TrimSpace(currentToken)))
	if err != nil {
		return fmt.Errorf("replace password and revoke sessions: %w", err)
	}
	for _, hash := range hashes {
		s.sessionRevoker.RevokeSessionHash(hash)
	}
	return nil
}

func (s *Service) ListSessions(ctx context.Context, userID int64, currentToken string) ([]SessionView, error) {
	records, err := s.sessions.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	current := hashSessionToken(strings.TrimSpace(currentToken))
	views := make([]SessionView, 0, len(records))
	for _, r := range records {
		views = append(views, SessionView{
			ID: r.ID, CreatedAt: r.CreatedAt, LastUsedAt: r.LastUsedAt, ExpiresAt: r.ExpiresAt,
			UserAgent: r.UserAgent, ClientIP: r.ClientIP, Current: string(r.TokenHash) == string(current),
		})
	}
	return views, nil
}

func (s *Service) RevokeSessionByID(ctx context.Context, userID, sessionID int64) error {
	hash, err := s.sessions.DeleteByID(ctx, userID, sessionID)
	if err != nil {
		return err
	}
	s.sessionRevoker.RevokeSessionHash(hash)
	return nil
}

func (s *Service) RevokeOtherSessions(ctx context.Context, userID int64, currentToken string) error {
	return s.revokeOthers(ctx, userID, hashSessionToken(strings.TrimSpace(currentToken)))
}

func (s *Service) revokeOthers(ctx context.Context, userID int64, keep []byte) error {
	hashes, err := s.sessions.DeleteOthers(ctx, userID, keep)
	if err != nil {
		return fmt.Errorf("delete other sessions: %w", err)
	}
	for _, hash := range hashes {
		s.sessionRevoker.RevokeSessionHash(hash)
	}
	return nil
}
