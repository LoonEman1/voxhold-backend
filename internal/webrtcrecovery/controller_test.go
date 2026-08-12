package webrtcrecovery

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestControllerStopsDelayedRecovery(t *testing.T) {
	controller := New(Policy{
		InitialDelay: 40 * time.Millisecond,
		RetryDelay:   time.Millisecond,
		MaxAttempts:  2,
	})
	var attempts atomic.Int32
	controller.Start(false, func() error {
		attempts.Add(1)
		return nil
	}, func() {
		t.Error("stopped recovery must not be exhausted")
	})
	controller.Stop()
	time.Sleep(60 * time.Millisecond)
	if value := attempts.Load(); value != 0 {
		t.Fatalf("unexpected recovery attempts: %d", value)
	}
}

func TestControllerRetriesAndExhausts(t *testing.T) {
	controller := New(Policy{
		RetryDelay:  5 * time.Millisecond,
		MaxAttempts: 3,
	})
	var attempts atomic.Int32
	exhausted := make(chan struct{})
	controller.Start(true, func() error {
		attempts.Add(1)
		return nil
	}, func() {
		close(exhausted)
	})

	select {
	case <-exhausted:
	case <-time.After(time.Second):
		t.Fatal("recovery did not finish")
	}
	if value := attempts.Load(); value != 3 {
		t.Fatalf("recovery attempts = %d, want 3", value)
	}
}

func TestImmediateRecoveryReplacesDelayedRecovery(t *testing.T) {
	controller := New(Policy{
		InitialDelay: time.Second,
		RetryDelay:   time.Millisecond,
		MaxAttempts:  1,
	})
	var attempts atomic.Int32
	exhausted := make(chan struct{})
	controller.Start(false, func() error {
		attempts.Add(100)
		return nil
	}, func() {
		t.Error("replaced recovery must not be exhausted")
	})
	controller.Start(true, func() error {
		attempts.Add(1)
		return nil
	}, func() {
		close(exhausted)
	})

	select {
	case <-exhausted:
	case <-time.After(time.Second):
		t.Fatal("immediate recovery did not finish")
	}
	if value := attempts.Load(); value != 1 {
		t.Fatalf("recovery attempts = %d, want 1", value)
	}
}

func TestControllerAllowsFinalAttemptToRecover(t *testing.T) {
	controller := New(Policy{
		RetryDelay:  50 * time.Millisecond,
		MaxAttempts: 1,
	})
	attempted := make(chan struct{})
	var exhausted atomic.Bool
	controller.Start(true, func() error {
		close(attempted)
		return nil
	}, func() {
		exhausted.Store(true)
	})
	select {
	case <-attempted:
	case <-time.After(time.Second):
		t.Fatal("final recovery attempt did not start")
	}
	controller.Stop()
	time.Sleep(70 * time.Millisecond)
	if exhausted.Load() {
		t.Fatal("recovered final attempt was reported as exhausted")
	}
}
