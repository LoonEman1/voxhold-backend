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

	// Another user's session cannot be deleted through someone else's id.
	strangerViews, _ := service.ListSessions(ctx, other.ID, strangerToken)
	if err = service.RevokeSessionByID(ctx, user.ID, strangerViews[0].ID); !errors.Is(err, account.ErrSessionNotFound) {
		t.Fatalf("foreign session err=%v", err)
	}
	if _, err = service.Authenticate(ctx, strangerToken); err != nil {
		t.Fatalf("foreign session was affected: %v", err)
	}
}
