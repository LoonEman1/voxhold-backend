//go:build !windows && !linux

package storagebudget

import "errors"

func Free(string) (int64, error) { return 0, errors.New("disk budget is unsupported on this platform") }
