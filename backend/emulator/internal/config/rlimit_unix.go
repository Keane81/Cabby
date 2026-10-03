//go:build unix

package config

import (
	"fmt"
	"syscall"
)

// macOS refuses a soft limit above this, whatever the hard limit says (OPEN_MAX).
const darwinOpenMax = 10240

// RaiseFileLimit makes sure the process may hold at least need open files: every pooled
// connection is one. It raises the soft limit as far as the system allows and fails with a clear
// message when that is not enough, instead of letting the run die in the middle with
// "too many open files" (research.md R-02).
func RaiseFileLimit(need uint64) error {
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		return fmt.Errorf("config: reading the open files limit: %w", err)
	}
	if limit.Cur >= need {
		return nil
	}
	for _, target := range []uint64{min(need, limit.Max), min(need, darwinOpenMax)} {
		raised := syscall.Rlimit{Cur: target, Max: limit.Max}
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &raised); err == nil {
			if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err == nil && limit.Cur >= need {
				return nil
			}
		}
	}
	return fmt.Errorf("config: the open files limit is %d and %d are needed: raise it (ulimit -n) or lower -max-conns", limit.Cur, need)
}
