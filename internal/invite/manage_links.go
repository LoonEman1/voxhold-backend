package invite

import (
	"context"
	"fmt"
)

func (s *Service) ListActiveLinks(ctx context.Context, serverID, userID int64, input ListLinksInput) (LinkPage, error) {
	if serverID <= 0 || userID <= 0 {
		return LinkPage{}, ErrForbidden
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	if input.Limit < 1 || input.Limit > 100 || (input.BeforeID != nil && *input.BeforeID <= 0) {
		return LinkPage{}, ErrLinkPaginationInvalid
	}
	links, err := s.repository.ListActiveLinks(ctx, serverID, userID, input.BeforeID, input.Limit+1)
	if err != nil {
		return LinkPage{}, fmt.Errorf("list active invite links: %w", err)
	}
	page := LinkPage{Links: links, HasMore: len(links) > input.Limit}
	if page.HasMore {
		page.Links = links[:input.Limit]
		lastID := page.Links[len(page.Links)-1].ID
		page.NextBeforeID = &lastID
	}
	if page.Links == nil {
		page.Links = []InviteLink{}
	}
	return page, nil
}

func (s *Service) RevokeLink(ctx context.Context, serverID, linkID, userID int64) error {
	if serverID <= 0 || userID <= 0 {
		return ErrForbidden
	}
	if linkID <= 0 {
		return ErrLinkNotFound
	}
	if err := s.repository.RevokeLink(ctx, serverID, linkID, userID); err != nil {
		return fmt.Errorf("revoke invite link: %w", err)
	}
	return nil
}
