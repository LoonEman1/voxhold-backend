package attachments

import (
	"context"
	"errors"
	"io"
	"time"
)

var errImageBusy = errors.New("image processing busy; retry shortly")

func (s *Service) acquireUserUpload(userID int64) bool {
	s.uploadUsersMu.Lock()
	defer s.uploadUsersMu.Unlock()
	if s.userUploads[userID] >= 2 {
		return false
	}
	s.userUploads[userID]++
	return true
}
func (s *Service) releaseUserUpload(userID int64) {
	s.uploadUsersMu.Lock()
	defer s.uploadUsersMu.Unlock()
	if s.userUploads[userID] <= 1 {
		delete(s.userUploads, userID)
	} else {
		s.userUploads[userID]--
	}
}

func (s *Service) compressUpload(ctx context.Context, file io.ReadSeeker) ([]byte, string, error) {
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case s.imageSlots <- struct{}{}:
		defer func() { <-s.imageSlots }()
	case <-ctx.Done():
		return nil, "", ctx.Err()
	case <-timer.C:
		return nil, "", errImageBusy
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	return compressImage(file)
}
