package safego

import (
	"errors"
	"testing"
	"time"
)

func TestRunExecutesFunction(t *testing.T) {
	executed := false
	Run("test", func() { executed = true })
	if !executed {
		t.Fatal("expected fn to execute")
	}
}

func TestGuardedRunRecoversPanicAndCallsCleanup(t *testing.T) {
	cleanedUp := false
	cleanup := func() { cleanedUp = true }

	GuardedRun("test", func() {
		panic(errors.New("boom"))
	}, cleanup)

	if !cleanedUp {
		t.Fatal("expected onPanic cleanup to run after recovered panic")
	}
}

func TestGuardedRunKeepsRunningWithoutPanic(t *testing.T) {
	cleanedUp := false
	GuardedRun("test", func() {}, func() { cleanedUp = true })
	if cleanedUp {
		t.Fatal("cleanup must not run when no panic occurred")
	}
}

func TestGoRunsAsynchronously(t *testing.T) {
	done := make(chan struct{})
	Go("test", func() { close(done) })
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("goroutine did not finish")
	}
}
