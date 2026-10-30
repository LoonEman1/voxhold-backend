package main

import "os"

func cgroupMemoryLimit() (int64, bool) {
	data, err := os.ReadFile("/sys/fs/cgroup/memory.max")
	if err != nil {
		return 0, false
	}
	return parseMemoryLimit(string(data))
}
