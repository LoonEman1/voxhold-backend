package sqlite

import (
	"context"
	"testing"

	"voxhold-backend/internal/testdb"
)

func TestBackfillKeepsConflictsNullAndLoginIsCaseInsensitive(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash) VALUES(60,'Admin2','h'),(61,'admin2','h')`); err != nil {
		t.Fatal(err)
	}
	if err := BackfillUsernameKeys(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := BackfillUsernameKeys(ctx, db); err != nil {
		t.Fatal(err)
	}
	var withKey int
	if err := db.QueryRow(`SELECT count(*) FROM users WHERE id IN(60,61) AND username_key='admin2'`).Scan(&withKey); err != nil || withKey != 1 {
		t.Fatalf("withKey=%d err=%v", withKey, err)
	}
	user, err := NewUserRepository(db).FindByUsername(ctx, "ADMIN2")
	if err != nil || user.Username != "Admin2" && user.Username != "admin2" {
		t.Fatalf("user=%+v err=%v", user, err)
	}
}
