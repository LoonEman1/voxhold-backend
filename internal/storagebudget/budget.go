// Package storagebudget owns the instance's shared disk allocation.
package storagebudget

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sync"
)

var ErrBudget = errors.New("storage allocations exceed available disk budget")

// Mu serializes reservations and budget changes across recordings and uploads.
var Mu sync.Mutex

type Category struct {
	Quota int64 `json:"quota_bytes"`
	Used  int64 `json:"used_bytes"`
}
type State struct {
	Available      int64               `json:"available_bytes"`
	Capacity       int64               `json:"capacity_bytes"`
	ReservePercent int                 `json:"reserve_percent"`
	Categories     map[string]Category `json:"categories"`
}

func Get(ctx context.Context, db *sql.DB, directory string) (State, error) {
	free, err := Free(directory)
	if err != nil {
		return State{}, err
	}
	v := State{Available: free, ReservePercent: 15, Categories: map[string]Category{}}
	rows, err := db.QueryContext(ctx, `SELECT category,COALESCE(sum(size_bytes),0) FROM attachments WHERE deleted_at IS NULL GROUP BY category UNION ALL SELECT 'recordings',COALESCE(sum(r.size_bytes+COALESCE(length(p.content),0)),0) FROM channel_recordings r LEFT JOIN recording_previews p ON p.recording_id=r.id WHERE r.status IN ('ready','uploading')`)
	if err != nil {
		return v, err
	}
	var used int64
	for rows.Next() {
		var cat string
		var size int64
		if err = rows.Scan(&cat, &size); err != nil {
			break
		}
		v.Categories[cat] = Category{Used: size}
		used += size
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return v, err
	}
	total := free
	if used > 0 && total <= math.MaxInt64-used {
		total += used
	}
	v.Capacity = total / 100 * 85
	var recording sql.NullInt64
	if err = db.QueryRowContext(ctx, `SELECT recording_quota_bytes FROM instance_settings WHERE id=1`).Scan(&recording); err != nil {
		return v, err
	}
	defaultRecord := min(int64(6<<30), v.Capacity/3)
	if recording.Valid {
		defaultRecord = min(recording.Int64, v.Capacity)
	}
	for _, cat := range []string{"images", "files", "recordings"} {
		c := v.Categories[cat]
		c.Quota = max(0, (v.Capacity-defaultRecord)/2)
		if cat == "recordings" {
			c.Quota = defaultRecord
		}
		err = db.QueryRowContext(ctx, `SELECT quota_bytes FROM storage_budgets WHERE category=?`, cat).Scan(&c.Quota)
		if err != nil && err != sql.ErrNoRows {
			return v, err
		}
		v.Categories[cat] = c
	}
	return v, nil
}

// Directory is shared with deployment's existing /app/data volume.
func Directory() string {
	if v := os.Getenv("ATTACHMENT_DIRECTORY"); v != "" {
		return v
	}
	return filepath.Join("data", "attachments")
}
