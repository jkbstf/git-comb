//go:build windows

package main

import (
	"os"
	"unsafe"
)

type consoleCoordinate struct {
	x, y int16
}

type consoleRectangle struct {
	left, top, right, bottom int16
}

type consoleScreenBufferInfo struct {
	size              consoleCoordinate
	cursorPosition    consoleCoordinate
	attributes        uint16
	window            consoleRectangle
	maximumWindowSize consoleCoordinate
}

var getConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")

func terminalColumns(file *os.File) (int, bool) {
	var info consoleScreenBufferInfo
	ok, _, _ := getConsoleScreenBufferInfo.Call(file.Fd(), uintptr(unsafe.Pointer(&info)))
	width := int(info.window.right-info.window.left) + 1
	return width, ok != 0 && width > 0
}
