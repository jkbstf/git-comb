//go:build !darwin && !windows

package comb

import "io/fs"

func nativeHidden(_ string, _ fs.DirEntry) bool { return false }
