//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package doclibrary

import (
	"errors"
	"os"
	"syscall"
)

// Kernel advisory locks are released automatically on process death. Lock files
// are deliberately never unlinked: replacing an inode can split lock ownership.
func tryFileLock(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}
