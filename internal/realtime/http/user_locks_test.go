package realtimehttp

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestUserLockSetSerializesSameUser(t *testing.T) {
	var locks userLockSet
	unlock := locks.lock(1)

	acquired := make(chan struct{})
	go func() {
		defer locks.lock(1)()
		close(acquired)
	}()

	select {
	case <-acquired:
		t.Fatal("same user acquired lock concurrently")
	case <-time.After(20 * time.Millisecond):
	}

	unlock()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("same user did not acquire released lock")
	}
}

func TestUserLockSetDoesNotBlockDifferentUsers(t *testing.T) {
	var locks userLockSet
	unlock := locks.lock(1)
	defer unlock()

	var acquired atomic.Bool
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		defer locks.lock(2)()
		acquired.Store(true)
	}()

	wait.Wait()
	if !acquired.Load() {
		t.Fatal("different user did not acquire independent lock")
	}
}
