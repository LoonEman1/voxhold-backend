// Package safego runs application-owned goroutines and Pion callbacks
// behind a panic boundary. A panic in media or realtime background work is
// logged with its stack and contained (optionally triggering a session
// cleanup) instead of terminating the whole process.
package safego

import (
	"log"
	"runtime/debug"
)

// Run executes fn synchronously behind a panic boundary. A recovered panic
// is logged with its stack; when onPanic is non-nil it is invoked afterwards
// so callers can tear down the damaged unit (e.g. fail a media session).
func GuardedRun(name string, fn func(), onPanic func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf(
				"recovered panic in %s: %v\n%s",
				name,
				recovered,
				debug.Stack(),
			)
			if onPanic != nil {
				onPanic()
			}
		}
	}()
	fn()
}

// Run executes fn synchronously behind a logging-only panic boundary.
func Run(name string, fn func()) {
	GuardedRun(name, fn, nil)
}

// Go spawns fn in a new goroutine behind a logging-only panic boundary.
func Go(name string, fn func()) {
	go GuardedRun(name, fn, nil)
}

// GuardedGo spawns fn in a new goroutine behind a panic boundary that also
// invokes onPanic after a recovered panic.
func GuardedGo(name string, fn func(), onPanic func()) {
	go GuardedRun(name, fn, onPanic)
}
