//go:build windows

package comb

import (
	"io/fs"
	"syscall"
)

const (
	fileAttributeHidden = 0x00000002
	fileAttributeSystem = 0x00000004
)

// nativeHidden reports directories Windows marks hidden or system. Directory
// enumeration already supplies these attributes to DirEntry.Info.
func nativeHidden(_ string, entry fs.DirEntry) bool {
	info, err := entry.Info()
	if err != nil {
		return false
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&(fileAttributeHidden|fileAttributeSystem) != 0
}
