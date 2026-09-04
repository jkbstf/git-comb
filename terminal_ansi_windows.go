//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

const enableVirtualTerminalProcessing = 0x0004

var (
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	getConsoleMode = kernel32.NewProc("GetConsoleMode")
	setConsoleMode = kernel32.NewProc("SetConsoleMode")
)

// terminalANSI enables virtual-terminal processing when the Windows console
// supports it. Older consoles still receive a carriage-return-only fallback.
func terminalANSI(file *os.File) bool {
	var mode uint32
	ok, _, _ := getConsoleMode.Call(file.Fd(), uintptr(unsafe.Pointer(&mode)))
	if ok == 0 {
		return false
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return true
	}
	ok, _, _ = setConsoleMode.Call(file.Fd(), uintptr(mode|enableVirtualTerminalProcessing))
	return ok != 0
}
