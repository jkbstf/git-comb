//go:build darwin || linux

package main

import (
	"os"
	"syscall"
	"unsafe"
)

type windowSize struct {
	rows, columns, widthPixels, heightPixels uint16
}

func terminalColumns(file *os.File) (int, bool) {
	var size windowSize
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		file.Fd(),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&size)),
	)
	return int(size.columns), errno == 0 && size.columns > 0
}
