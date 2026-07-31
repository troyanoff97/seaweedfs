package storage

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

func TestIsDiskError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"generic", errors.New("something else"), false},
		{"io string", errors.New("input/output error"), true},
		{"readonly", errors.New("read-only file system"), true},
		{"enospc", errors.New("no space left on device"), true},
		{"not writable", errors.New("Not writable!"), true},
		{"errno enospc", syscall.Errno(syscall.ENOSPC), true},
		{"path error", &os.PathError{Op: "write", Path: "/data/x.dat", Err: syscall.EIO}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsDiskError(tc.err); got != tc.want {
				t.Fatalf("IsDiskError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
