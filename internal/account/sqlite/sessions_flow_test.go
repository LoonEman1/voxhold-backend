package sqlite

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"voxhold-backend/internal/account"
	"voxhold-backend/internal/testdb"
)

type recordingRevoker struct{ hashes [][]byte }

func (r *recordingRevoker) RevokeSession(string)       {}
func (r *recordingRevoker) RevokeSessionHash(h []byte) { r.hashes = append(r.hashes, h) }

func TestPasswordChangeAndSessionManagement(t *testing.T) {
	db := testdb.Open(t)
	ctx := account.WithClientMeta(context.Background(), account.ClientMeta{UserAgent: "UA", IP: "203.0.113.1"})
	hash, _ := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.MinCost)
	users := NewUserRepository(db)
	user, err := users.Create(ctx, "sessionuser", string(hash))
	if err != nil {
		t.Fatal(err)
	}
	other, err := users.Create(ctx, "stranger", string(hash))
	if err != nil {
		t.Fatal(err)
	}
	revoker := &recordingRevoker{}
	service := account.NewService(users, NewSessionRepository(db), nil, revoker, nil, nil)

	login := func(name string) string {
		t.Helper()
		result, err := service.Login(ctx, account.LoginInput{Username: name, Password: "old-password"})
		if err != nil {
			t.Fatal(err)
		}
		return result.Session.Token
	}
	current, second := login("sessionuser"), login("sessionuser")
	strangerToken := login("stranger")

	views, err := service.ListSessions(ctx, user.ID, current)
	if err != nil || len(views) != 2 {
		t.Fatalf("views=%v err=%v", views, err)
	}
	currents := 0
	for _, v := range views {
		if v.Current {
			currents++
		}
		if v.UserAgent != "UA" || v.ClientIP != "203.0.113.1" {
			t.Fatalf("metadata lost: %+v", v)
		}
	}
	if currents != 1 {
		t.Fatalf("current flags = %d", currents)
	}

	bad := account.ChangePasswordInput{CurrentPassword: "wrong", NewPassword: "new-password", NewConfirm: "new-password"}
	if err = service.ChangePassword(ctx, user.ID, current, bad); !errors.Is(err, account.ErrWrongCurrentPassword) {
		t.Fatalf("wrong password err=%v", err)
	}
	good := account.ChangePasswordInput{CurrentPassword: "old-password", NewPassword: "new-password", NewConfirm: "new-password"}
	// A database failure while revoking devices must roll back the password too.
	if _, err = db.ExecContext(ctx, `CREATE TRIGGER fail_session_revoke BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT, 'test revoke failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = service.ChangePassword(ctx, user.ID, current, good); err == nil {
		t.Fatal("password change must fail when session deletion fails")
	}
	stored, err := users.FindByID(ctx, user.ID)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte("old-password")) != nil {
		t.Fatalf("failed revocation changed the password: %v", err)
	}
	for _, token := range []string{current, second} {
		if _, err = service.Authenticate(ctx, token); err != nil {
			t.Fatalf("rolled-back change revoked a session: %v", err)
		}
	}
	if len(revoker.hashes) != 0 {
		t.Fatalf("failed transaction revoked %d websocket sessions", len(revoker.hashes))
	}
	if _, err = db.ExecContext(ctx, `DROP TRIGGER fail_session_revoke`); err != nil {
		t.Fatal(err)
	}
	if err = service.ChangePassword(ctx, user.ID, current, good); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Authenticate(ctx, current); err != nil {
		t.Fatalf("current session must survive: %v", err)
	}
	if _, err = service.Authenticate(ctx, second); !errors.Is(err, account.ErrUnauthorized) {
		t.Fatalf("other session must be revoked: %v", err)
	}
	if len(revoker.hashes) != 1 {
		t.Fatalf("websocket revocations = %d", len(revoker.hashes))
	}
	if _, err = NewSessionRepository(db).ReplacePasswordAndRevokeOthers(ctx, user.ID, string(hash), "stale-change", nil); !errors.Is(err, account.ErrWrongCurrentPassword) {
		t.Fatalf("stale credentials overwrote the changed password: %v", err)
	}
	if _, err = service.Authenticate(ctx, current); err != nil {
		t.Fatalf("stale change affected the current session: %v", err)
	}
	if err = service.VerifyPassword(ctx, user.ID, "new-password"); err != nil {
		t.Fatalf("new password was lost after rejecting stale credentials: %v", err)
	}

	// Another user's session cannot be deleted through someone else's id.
	strangerViews, _ := service.ListSessions(ctx, other.ID, strangerToken)
	if err = service.RevokeSessionByID(ctx, user.ID, strangerViews[0].ID); !errors.Is(err, account.ErrSessionNotFound) {
		t.Fatalf("foreign session err=%v", err)
	}
	if _, err = service.Authenticate(ctx, strangerToken); err != nil {
		t.Fatalf("foreign session was affected: %v", err)
	}
}
