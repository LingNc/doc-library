//go:build windows

package doclibrary

import (
	"os"
	"syscall"
	"unsafe"
)

var lockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")

func tryFileLock(f *os.File) (bool, error) {
	var ov syscall.Overlapped
	ok, _, err := lockFileEx.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&ov)))
	if ok != 0 {
		return true, nil
	}
	if err == syscall.Errno(33) || err == syscall.Errno(32) {
		return false, nil
	}
	return false, err
}
