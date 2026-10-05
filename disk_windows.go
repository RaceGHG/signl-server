//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

func diskSpace(dir string) (free, total int64) {
	k := syscall.NewLazyDLL("kernel32.dll")
	proc := k.NewProc("GetDiskFreeSpaceExW")
	p, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return 0, 0
	}
	var f, t, tf uint64
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&f)), uintptr(unsafe.Pointer(&t)), uintptr(unsafe.Pointer(&tf)))
	if r == 0 {
		return 0, 0
	}
	return int64(f), int64(t)
}
