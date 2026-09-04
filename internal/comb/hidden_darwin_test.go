//go:build darwin

package comb

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestScanHonorsDarwinHiddenFlag(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	base := tempDir(t)
	hidden := filepath.Join(base, "LibraryLike")
	initRepo(t, filepath.Join(hidden, "repo"))
	if err := syscall.Chflags(hidden, userHiddenFlag); err != nil {
		t.Skipf("setting hidden flag: %v", err)
	}

	withoutHidden, err := Scan([]string{base}, false, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(withoutHidden) != 0 {
		t.Errorf("default scan entered Finder-hidden directory: %v", withoutHidden)
	}
	withHidden, err := Scan([]string{base}, true, nil)
	if err != nil {
		t.Fatalf("Scan --hidden: %v", err)
	}
	if len(withHidden) != 1 {
		t.Errorf("--hidden found %d repositories, want 1: %v", len(withHidden), withHidden)
	}
}
