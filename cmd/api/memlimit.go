package main

import (
	"log"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

func parseMemoryLimit(data string) (int64, bool) {
	limit, err := strconv.ParseInt(strings.TrimSpace(data), 10, 64)
	return limit, err == nil && limit >= 10
}

func configureMemoryLimit() {
	if os.Getenv("GOMEMLIMIT") != "" {
		return
	}
	if limit, ok := cgroupMemoryLimit(); ok {
		budget := limit / 10 * 8
		debug.SetMemoryLimit(budget)
		log.Printf("memory limit set to %d bytes (80%% of cgroup limit)", budget)
	}
}
