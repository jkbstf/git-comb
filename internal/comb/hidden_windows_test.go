//go:build windows

package comb

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestScanHonorsWindowsHiddenAttribute(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	base := tempDir(t)
	for _, item := range []struct {
		name string
		attr uint32
	}{
		{"HiddenLike", fileAttributeHidden},
		{"SystemLike", fileAttributeSystem},
	} {
		dir := filepath.Join(base, item.name)
		initRepo(t, filepath.Join(dir, "repo"))
		name, err := syscall.UTF16PtrFromString(dir)
		if err != nil {
			t.Fatal(err)
		}
		attrs, err := syscall.GetFileAttributes(name)
		if err != nil {
			t.Skipf("reading attributes: %v", err)
		}
		if err := syscall.SetFileAttributes(name, attrs|item.attr); err != nil {
			t.Skipf("setting %s attribute: %v", item.name, err)
		}
	}

	withoutHidden, err := Scan([]string{base}, false, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(withoutHidden) != 0 {
		t.Errorf("default scan entered hidden directory: %v", withoutHidden)
	}
	withHidden, err := Scan([]string{base}, true, nil)
	if err != nil {
		t.Fatalf("Scan --hidden: %v", err)
	}
	if len(withHidden) != 2 {
		t.Errorf("--hidden found %d repositories, want 2: %v", len(withHidden), withHidden)
	}
}
