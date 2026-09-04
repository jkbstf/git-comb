//go:build !windows

package main

import "os"

func terminalANSI(_ *os.File) bool { return true }
