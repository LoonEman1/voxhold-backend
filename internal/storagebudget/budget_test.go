package storagebudget

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"voxhold-backend/internal/testdb"
)

func TestUsageQueryUsesCoveringIndexAndMigrationReverses(t *testing.T) {
	db := testdb.Open(t)
	rows, err := db.Query(`EXPLAIN QUERY PLAN SELECT category, COALESCE(sum(size_bytes),0) FROM attachments WHERE deleted_at IS NULL GROUP BY category`)
	if err != nil {
		t.Fatal(err)
	}
	covered := false
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		covered = covered || strings.Contains(detail, "USING COVERING INDEX attachments_usage")
	}
	err = rows.Err()
	rows.Close()
	if err != nil || !covered {
		t.Fatalf("usage query is not covered: %v", err)
	}
	for _, direction := range []string{"down", "up"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000033_attachment_dimensions."+direction+".sql"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(data)); err != nil {
			t.Fatal(direction, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO attachments(id,channel_id,owner_user_id,filename,mime_type,category,size_bytes,width,height) VALUES('invalid-dimensions',2,2,'x','image/png','images',1,0,1)`); err == nil {
		t.Fatal("accepted nonpositive dimensions")
	}
}
