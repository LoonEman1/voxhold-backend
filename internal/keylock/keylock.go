// Package keylock serializes transitions for one key without blocking other keys.
package keylock

import "sync"

type entry struct {
	mu   sync.Mutex
	refs int
}

type Set struct {
	mu      sync.Mutex
	entries map[int64]*entry
}

func (s *Set) Lock(key int64) func() {
	s.mu.Lock()
	if s.entries == nil {
		s.entries = make(map[int64]*entry)
	}
	value := s.entries[key]
	if value == nil {
		value = &entry{}
		s.entries[key] = value
	}
	value.refs++
	s.mu.Unlock()
	value.mu.Lock()
	return func() {
		value.mu.Unlock()
		s.mu.Lock()
		value.refs--
		if value.refs == 0 {
			delete(s.entries, key)
		}
		s.mu.Unlock()
	}
}
