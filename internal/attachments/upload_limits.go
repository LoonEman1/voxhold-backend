package attachments

import (
	"context"
	"errors"
	"io"
	"time"
)

var errImageBusy = errors.New("image processing busy; retry shortly")

func imageConcurrency(memoryLimit int64) int {
	// Two 24 MP CatmullRom jobs peak near 780 MiB; GC alone cannot bound
	// their live scaler scratch space on the default 768 MiB container.
	if memoryLimit > 0 && memoryLimit < 1<<30 {
		return 1
	}
	return 2
}

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

func (s *Service) compressUpload(ctx context.Context, file io.ReadSeeker) ([]byte, string, int, int, error) {
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case s.imageSlots <- struct{}{}:
		defer func() { <-s.imageSlots }()
	case <-ctx.Done():
		return nil, "", 0, 0, ctx.Err()
	case <-timer.C:
		return nil, "", 0, 0, errImageBusy
	}
	if err := ctx.Err(); err != nil {
		return nil, "", 0, 0, err
	}
	return compressImage(file)
}
