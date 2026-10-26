package sqlite_test

import (
	"context"
	"testing"
	serversqlite "voxhold-backend/internal/server/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestDeleteAccountClearsPrivateDataAndRetainsExpiringRecordings(t *testing.T) {
	db := testdb.Open(t)
	_, err := db.Exec(`INSERT INTO users(username,password_hash) VALUES('Удалённый #2','hash');
	INSERT INTO user_avatars(user_id,content,etag) VALUES(2,x'0102','avatar');
	INSERT INTO client_diagnostic_events(client_timestamp_ms,user_id,session_id,client_version,platform,category,event_name,level) VALUES(1,2,'session','1','web','test','test','info');
	INSERT INTO channel_recordings(id,server_id,channel_id,owner_user_id,status,mime_type,size_bytes,updated_at,expires_at) VALUES('01234567890123456789012345678901',1,1,2,'ready','video/webm',12,1,unixepoch()+86400);
	INSERT INTO recording_previews(recording_id,content,etag) VALUES('01234567890123456789012345678901',x'0102','preview');`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = serversqlite.NewRepository(db).DeleteAccount(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int{"user_avatars": 0, "client_diagnostic_events": 0, "channel_recordings": 1, "recording_previews": 1} {
		var n int
		if err = db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != want {
			t.Fatalf("%s count=%d want=%d: %v", table, n, want, err)
		}
	}
}
