// Package leaktest enables Go 1.27 runtime leak checks after a package's tests.
package leaktest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"testing"
	"time"
)

// Run preserves the normal test status and checks runtime-detected leaks when
// VOXHOLD_CHECK_GOROUTINE_LEAKS=1. TestMain must pass its result to os.Exit.
func Run(m *testing.M) int {
	status := m.Run()
	if os.Getenv("VOXHOLD_CHECK_GOROUTINE_LEAKS") != "1" {
		return status
	}
	profile := pprof.Lookup("goroutineleak")
	if profile == nil {
		fmt.Fprintln(os.Stderr, "goroutineleak profile unavailable: use Go 1.27 or newer")
		return 1
	}
	var report bytes.Buffer
	deadline := time.Now().Add(2 * time.Second)
	for {
		report.Reset()
		// Writing the profile triggers the runtime's GC-based leak detection.
		if err := profile.WriteTo(&report, 1); err != nil {
			fmt.Fprintln(os.Stderr, "goroutineleak profile:", err)
			return 1
		}
		if profile.Count() == 0 {
			return status
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Fprintln(os.Stderr, "runtime detected leaked goroutines after test cleanup:")
	_, _ = os.Stderr.Write(report.Bytes())
	if directory := os.Getenv("VOXHOLD_LEAK_REPORT_DIR"); directory != "" {
		wd, _ := os.Getwd()
		name := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
				return r
			}
			return '_'
		}, wd)
		if err := os.MkdirAll(directory, 0o700); err == nil {
			if err := os.WriteFile(filepath.Join(directory, name+".txt"), report.Bytes(), 0o600); err != nil {
				fmt.Fprintln(os.Stderr, "save goroutineleak report:", err)
			}
		}
	}
	return 1
}
