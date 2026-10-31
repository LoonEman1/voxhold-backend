package main

import (
	"context"
	"testing"
	"time"
)

func TestMaintenanceTimeoutDoesNotStarveJournal(t *testing.T) {
	pruned := false
	runStorageMaintenance(context.Background(),
		maintenanceTask{"attachments", time.Millisecond, func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }},
		maintenanceTask{"journal", time.Second, func(ctx context.Context) error { pruned = ctx.Err() == nil; return nil }},
	)
	if !pruned {
		t.Fatal("attachment timeout starved the journal")
	}
}
