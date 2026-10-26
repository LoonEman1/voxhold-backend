package sqlite_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"voxhold-backend/internal/account"
	accountsqlite "voxhold-backend/internal/account/sqlite"
	"voxhold-backend/internal/invite"
	invitesqlite "voxhold-backend/internal/invite/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestManagersListOnlyActiveServerLinksAndCanRevokeOtherCreators(t *testing.T) {
	db := testdb.OpenFile(t)
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash) VALUES(4,'admin','hash'),(6,'other-owner','hash');
	INSERT INTO server_members(server_id,user_id,role) VALUES(1,4,'admin');`); err != nil {
		t.Fatal(err)
	}
	repo := invitesqlite.NewRepository(db)
	ctx := context.Background()
	limit := 3
	create := func(name string, serverID, creator int64, expires int64, maximum *int) invite.InviteLink {
		t.Helper()
		hash := sha256.Sum256([]byte(name))
		link, err := repo.CreateLink(ctx, serverID, creator, hash[:], expires, maximum, maximum != nil)
		if err != nil {
			t.Fatal(err)
		}
		return link
	}
	future := time.Now().Add(time.Hour).Unix()
	owner := create("owner", 1, 1, future, &limit)
	admin := create("admin", 1, 4, future, &limit)
	unlimited := create("unlimited", 1, 1, future, nil)
	create("expired", 1, 1, time.Now().Add(-time.Hour).Unix(), &limit)
	exhausted := create("exhausted", 1, 1, future, &limit)
	revoked := create("revoked", 1, 1, future, &limit)
	if _, err := db.Exec("UPDATE server_invite_links SET use_count=max_uses WHERE id=?", exhausted.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.RevokeLink(ctx, 1, revoked.ID, 1); err != nil {
		t.Fatal(err)
	}
	for _, userID := range []int64{1, 4} {
		links, err := repo.ListActiveLinks(ctx, 1, userID, nil, 50)
		if err != nil || len(links) != 3 {
			t.Fatal(links, err)
		}
		if links[0].ID != unlimited.ID || links[1].ID != admin.ID || links[2].ID != owner.ID {
			t.Fatal(links)
		}
		for _, link := range links {
			if link.Token != "" || link.ServerID != 1 {
				t.Fatal("management list leaked a token or another server", link)
			}
		}
	}
	page, err := repo.ListActiveLinks(ctx, 1, 4, &unlimited.ID, 1)
	if err != nil || len(page) != 1 || page[0].ID != admin.ID {
		t.Fatal(page, err)
	}
	for _, userID := range []int64{2, 3, 6} {
		if _, err := repo.ListActiveLinks(ctx, 1, userID, nil, 50); !errors.Is(err, invite.ErrForbidden) {
			t.Fatal(userID, err)
		}
		if err := repo.RevokeLink(ctx, 1, owner.ID, userID); !errors.Is(err, invite.ErrForbidden) {
			t.Fatal(userID, err)
		}
	}
	if err := repo.RevokeLink(ctx, 1, 999999, 4); !errors.Is(err, invite.ErrLinkNotFound) {
		t.Fatal(err)
	}
	if err := repo.RevokeLink(ctx, 2, owner.ID, 1); !errors.Is(err, invite.ErrForbidden) {
		t.Fatal("wrong server scope was accepted", err)
	}
	if err := repo.RevokeLink(ctx, 1, owner.ID, 4); err != nil {
		t.Fatal(err)
	}
	if err := repo.RevokeLink(ctx, 1, owner.ID, 1); err != nil {
		t.Fatal("repeat revoke must be idempotent", err)
	}
	ownerHash := sha256.Sum256([]byte("owner"))
	if _, err := repo.ResolveLink(ctx, ownerHash[:]); !errors.Is(err, invite.ErrLinkInvalid) {
		t.Fatal(err)
	}
	if _, _, _, err := repo.AcceptLink(ctx, ownerHash[:], 3); !errors.Is(err, invite.ErrLinkInvalid) {
		t.Fatal(err)
	}
	adminHash := sha256.Sum256([]byte("admin"))
	if _, err := repo.ResolveLink(ctx, adminHash[:]); err != nil {
		t.Fatal("unrelated link was changed", err)
	}
	if _, err := db.Exec("UPDATE server_members SET role='member' WHERE server_id=1 AND user_id=4"); err != nil {
		t.Fatal(err)
	}
	if err := repo.RevokeLink(ctx, 1, admin.ID, 4); !errors.Is(err, invite.ErrForbidden) {
		t.Fatal("demoted creator retained revoke access", err)
	}
}

func TestRevokedInviteCannotRegisterNewAccounts(t *testing.T) {
	db := testdb.OpenFile(t)
	repo := invitesqlite.NewRepository(db)
	registration := accountsqlite.NewInviteRegistrationRepository(db)
	ctx := context.Background()
	hash := sha256.Sum256([]byte("registration-to-revoke"))
	limit := 20
	link, err := repo.CreateLink(ctx, 1, 1, hash[:], time.Now().Add(time.Hour).Unix(), &limit, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RevokeLink(ctx, 1, link.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := registration.ValidateRegistrationInvite(ctx, hash[:]); !errors.Is(err, account.ErrRegistrationInviteInvalid) {
		t.Fatal(err)
	}
	results := make(chan error, 8)
	for index := 0; index < 8; index++ {
		go func(index int) {
			sessionHash := sha256.Sum256([]byte(fmt.Sprintf("session-%d", index)))
			_, _, _, err := registration.RegisterWithInvite(ctx, fmt.Sprintf("revoked-user-%d", index), "hash", hash[:], sessionHash[:], time.Now().Add(time.Hour).Unix())
			results <- err
		}(index)
	}
	for index := 0; index < 8; index++ {
		if err := <-results; !errors.Is(err, account.ErrRegistrationInviteInvalid) {
			t.Fatal(err)
		}
	}
	var uses, accounts int
	if err := db.QueryRow("SELECT use_count FROM server_invite_links WHERE id=?", link.ID).Scan(&uses); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM users WHERE username LIKE 'revoked-user-%'").Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	if uses != 0 || accounts != 0 {
		t.Fatal(uses, accounts)
	}
}
