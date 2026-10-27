package leaktest

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(Run(m)) }

func TestRuntimeGateRejectsAnUnreachableBlockedGoroutine(t *testing.T) {
	for _, leak := range []string{"0", "1"} {
		command := exec.Command(os.Args[0], "-test.run=^TestLeakFixture$")
		command.Env = append(os.Environ(), "VOXHOLD_CHECK_GOROUTINE_LEAKS=1", "VOXHOLD_LEAK_REPORT_DIR=", "VOXHOLD_LEAK_FIXTURE="+leak)
		output, err := command.CombinedOutput()
		if leak == "0" && err != nil {
			t.Fatalf("clean process rejected: %v\n%s", err, output)
		}
		if leak == "1" && (err == nil || !strings.Contains(string(output), "runtime detected leaked goroutines")) {
			t.Fatalf("leaked goroutine did not fail the process: %v\n%s", err, output)
		}
	}
}

func TestLeakFixture(t *testing.T) {
	if os.Getenv("VOXHOLD_LEAK_FIXTURE") != "1" {
		return
	}
	started := make(chan struct{})
	go func() {
		blocked := make(chan struct{})
		close(started)
		<-blocked
	}()
	<-started
}
