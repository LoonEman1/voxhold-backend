package realtimehttp

import "sync"

type userLock struct {
	mu   sync.Mutex
	refs int
}

type userLockSet struct {
	mu    sync.Mutex
	locks map[int64]*userLock
}

func (s *userLockSet) lock(userID int64) func() {
	s.mu.Lock()
	if s.locks == nil {
		s.locks = make(map[int64]*userLock)
	}

	value := s.locks[userID]
	if value == nil {
		value = &userLock{}
		s.locks[userID] = value
	}
	value.refs++
	s.mu.Unlock()

	value.mu.Lock()

	return func() {
		value.mu.Unlock()

		s.mu.Lock()
		value.refs--
		if value.refs == 0 {
			delete(s.locks, userID)
		}
		s.mu.Unlock()
	}
}
