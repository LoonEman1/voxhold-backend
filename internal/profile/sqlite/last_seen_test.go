package sqlite_test

import (
	"context"
	"testing"

	profilesqlite "voxhold-backend/internal/profile/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestRecordLastSeenPreservesProfileAndIsMonotonic(t *testing.T) {
	db := testdb.Open(t)
	if _, err := db.Exec("INSERT INTO user_profiles(user_id,about,country_code,last_seen_at) VALUES(1,'About','RU',10)"); err != nil {
		t.Fatal(err)
	}
	repo := profilesqlite.NewRepository(db)
	for _, at := range []int64{20, 30, 15} {
		if err := repo.RecordLastSeen(context.Background(), 1, at); err != nil {
			t.Fatal(err)
		}
	}
	profile, err := repo.GetByUserID(context.Background(), 1)
	if err != nil || profile.LastSeenAt == nil || *profile.LastSeenAt != 30 || profile.About != "About" || profile.CountryCode == nil || *profile.CountryCode != "RU" {
		t.Fatal(profile, err)
	}
	if err := repo.RecordLastSeen(context.Background(), 999, 100); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM user_profiles WHERE user_id=999").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}
