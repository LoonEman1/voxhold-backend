package recordings

import (
	"testing"
	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"
)

func TestChannelRestrictionRevokesMetadataPreviewAndIssuedDownloadTicket(t *testing.T) {
	s := setup(t)
	record := ready(t, s)
	ticket, err := s.Ticket(ctx, record.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	repo := channelsqlite.NewRepository(s.db)
	if _, err := repo.SetAccess(ctx, 1, 1, 1, channel.Access{Restricted: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, record.ID, 2); err == nil {
		t.Fatal("metadata leaked")
	}
	listed, err := s.List(ctx, 1, 2)
	if err != nil || len(listed) != 0 {
		t.Fatalf("list leaked: %v %v", listed, err)
	}
	if _, _, err := s.OpenDownload(ctx, ticket); err == nil {
		t.Fatal("old ticket retained access")
	}
	if _, err := s.Start(ctx, 1, 1, 2, "video/webm"); err == nil {
		t.Fatal("revoked member started a recording")
	}
}
