package recordings

import (
	"context"
	"errors"
	"os"
)

// Delete removes the server copy only. Browser copies remain under the user's control.
func (s *Service) Delete(ctx context.Context, id string, userID int64) (Record, error) {
	return s.delete(ctx, id, userID, false)
}

func (s *Service) DeleteForOwner(ctx context.Context, id string, userID int64) (Record, error) {
	if err := s.requireOwner(ctx, userID); err != nil {
		return Record{}, err
	}
	return s.delete(ctx, id, userID, true)
}
func (s *Service) delete(ctx context.Context, id string, userID int64, instanceOwner bool) (Record, error) {
	s.mu.Lock()
	r, err := s.Get(ctx, id, userID)
	if err != nil {
		s.mu.Unlock()
		return r, err
	}
	if r.OwnerUserID != userID && !instanceOwner {
		s.mu.Unlock()
		return r, ErrNotFound
	}
	if r.Status == "recording" {
		s.mu.Unlock()
		return r, ErrConflict
	}
	for _, name := range []string{id + ".part", id + r.extension()} {
		if err = s.root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.mu.Unlock()
			return r, err
		}
	}
	// recording_previews is removed by the foreign-key cascade in this statement.
	_, err = s.db.ExecContext(ctx, `DELETE FROM channel_recordings WHERE id=?`, id)
	s.mu.Unlock()
	if err != nil {
		return r, err
	}
	r.Status = "deleted"
	r.PreviewVersion = nil
	r.UpdatedAt = max(r.UpdatedAt+1, s.now().UnixMilli())
	s.emit(r)
	return r, nil
}
