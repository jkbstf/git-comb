package comb

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Scan walks the roots and returns every directory containing a .git
// entry. A .git directory marks an ordinary repository; a .git file
// marks a linked worktree or a submodule checkout. The walk descends
// into repositories, so nested checkouts are found too, but never
// into .git itself. Hidden directories are skipped unless hidden is
// set; node_modules and every directory whose name matches a prune
// glob are always skipped. An unreadable subtree is skipped rather
// than aborting the scan.
//
// Roots are resolved through symlinks before walking — a symlinked
// root must scan what it points at, not silently scan nothing — and a
// root that does not resolve to a directory is an error: a mistyped
// root must never look like a clean tree.
func Scan(roots []string, hidden bool, prune []string) ([]string, error) {
	patterns := append([]string{"node_modules"}, prune...)
	for _, p := range patterns {
		if _, err := path.Match(p, "probe"); err != nil {
			return nil, fmt.Errorf("prune: bad pattern %q", p)
		}
	}

	var repos []string
	seen := map[string]bool{}
	for _, given := range roots {
		root, err := resolveRoot(given)
		if err != nil {
			return nil, err
		}
		walk := func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if path == root {
					return err
				}
				return nil
			}
			if d.Name() == ".git" {
				repo := filepath.Dir(path)
				if abs, aerr := filepath.Abs(repo); aerr == nil && !seen[abs] {
					seen[abs] = true
					repos = append(repos, repo)
				}
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if !d.IsDir() || path == root {
				return nil
			}
			if matchesAny(d.Name(), patterns) {
				return fs.SkipDir
			}
			if !hidden && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if err := filepath.WalkDir(root, walk); err != nil {
			return nil, err
		}
	}
	return repos, nil
}

// matchesAny reports whether name matches one of the prune globs.
// The patterns were validated up front, so Match cannot fail here.
func matchesAny(name string, patterns []string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}

// resolveRoot follows symlinks and insists on a directory.
func resolveRoot(root string) (string, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("root %s: %w", root, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("root %s: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("root %s is not a directory", root)
	}
	return filepath.Clean(resolved), nil
}
