package storage

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"syscall"
)

// IsDiskError reports whether err indicates a local filesystem or mount problem.
// Read and write paths use this to mark a DiskLocation unhealthy without stopping
// the volume server process.
func IsDiskError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, syscall.EIO) ||
		errors.Is(err, syscall.ENOSPC) ||
		errors.Is(err, syscall.EROFS) ||
		errors.Is(err, syscall.EPERM) ||
		errors.Is(err, fs.ErrPermission) {
		return true
	}

	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		if errno, ok := pathErr.Err.(syscall.Errno); ok {
			switch errno {
			case syscall.EIO, syscall.ENOSPC, syscall.EROFS, syscall.EPERM:
				return true
			}
		}
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "input/output error") ||
		strings.Contains(msg, "read-only file system") ||
		strings.Contains(msg, "no space left on device") ||
		strings.Contains(msg, "not writable")
}
