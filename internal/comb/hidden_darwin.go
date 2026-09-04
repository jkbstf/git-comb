//go:build darwin

package comb

import (
	"io/fs"
	"syscall"
)

const userHiddenFlag = 0x00008000

// nativeHidden reports Finder-hidden directories. DirEntry.Info is a stat on
// Darwin, but skipping trees such as ~/Library pays that cost back quickly.
func nativeHidden(_ string, entry fs.DirEntry) bool {
	info, err := entry.Info()
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Flags&userHiddenFlag != 0
}
