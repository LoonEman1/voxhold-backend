//go:build !linux

package medialoadstats

func CPUSeconds() float64 { return -1 }
func RSSBytes() int64     { return -1 }
