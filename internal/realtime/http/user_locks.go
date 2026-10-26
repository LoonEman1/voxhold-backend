package realtimehttp

import "voxhold-backend/internal/keylock"

type userLockSet struct{ set keylock.Set }

func (s *userLockSet) lock(userID int64) func() { return s.set.Lock(userID) }
