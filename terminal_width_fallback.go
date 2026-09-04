//go:build !darwin && !linux && !windows

package main

import "os"

func terminalColumns(_ *os.File) (int, bool) {
	return 0, false
}
