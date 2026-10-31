//go:build !linux

package main

func cgroupMemoryLimit() (int64, bool) { return 0, false }
