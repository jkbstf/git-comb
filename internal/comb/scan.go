package comb

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// Scan walks the roots and returns every directory containing a .git
// entry. A .git directory marks an ordinary repository; a .git file
// marks a linked worktree or a submodule checkout. The walk descends
// into repositories, so nested checkouts are found too, but never
// into .git itself. Hidden directories are skipped unless hidden is
// set; node_modules and every name in prune are always skipped. An
// unreadable subtree is skipped rather than aborting the scan; only a
// broken root is an error.
func Scan(roots []string, hidden bool, prune []string) ([]string, error) {
	skip := map[string]bool{"node_modules": true}
	for _, name := range prune {
		skip[name] = true
	}

	var repos []string
	seen := map[string]bool{}
	for _, root := range roots {
		root := filepath.Clean(root)
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
			if skip[d.Name()] {
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
