package storagebudget

import "golang.org/x/sys/unix"

func Free(path string) (int64, error) {
	var stat unix.Statfs_t
	err := unix.Statfs(path, &stat)
	return int64(stat.Bavail) * stat.Bsize, err
}
