package comb

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Scan walks the roots and returns every directory containing a .git entry,
// sorted by path. A .git directory marks an ordinary repository; a .git file
// marks a linked worktree or a submodule checkout. The walk descends
// into repositories, so nested checkouts are found too, but never
// into .git itself. Dot-prefixed and platform-hidden/system directories are
// skipped unless hidden is set; node_modules and every directory whose name
// matches a prune glob are always skipped. An unreadable subtree is skipped
// rather than aborting the scan.
//
// Roots are resolved through symlinks before walking — a symlinked
// root must scan what it points at, not silently scan nothing — and a
// root that does not resolve to a directory is an error: a mistyped
// root must never look like a clean tree.
func Scan(roots []string, hidden bool, prune []string) ([]string, error) {
	repos, _, err := scan(roots, hidden, prune, nil, nil)
	slices.Sort(repos)
	return repos, err
}

type scanStats struct {
	entries, directories, hiddenSkipped, depthSkipped, pruned, unreadable int
}

func scan(roots []string, hidden bool, prune []string, maxDepth *int, progress ProgressFunc) ([]string, scanStats, error) {
	patterns, err := newPrunePatterns(prune)
	if err != nil {
		return nil, scanStats{}, err
	}

	var repos []string
	var stats scanStats
	seen := map[string]bool{}
	for _, given := range roots {
		root, err := resolveRoot(given)
		if err != nil {
			return nil, stats, err
		}
		stats.entries++
		stats.directories++
		reportDiscovery(progress, root, stats, len(repos), true)
		if err := scanRoot(root, hidden, patterns, maxDepth, seen, &repos, &stats, progress); err != nil {
			return nil, stats, err
		}
	}
	return repos, stats, nil
}

type prunePatterns struct {
	exact map[string]bool
	globs []string
}

func newPrunePatterns(prune []string) (prunePatterns, error) {
	patterns := append([]string{"node_modules"}, prune...)
	compiled := prunePatterns{exact: make(map[string]bool, len(patterns))}
	for _, pattern := range patterns {
		if _, err := path.Match(pattern, "probe"); err != nil {
			return prunePatterns{}, fmt.Errorf("prune: bad pattern %q", pattern)
		}
		if strings.ContainsAny(pattern, `*?[\`) {
			compiled.globs = append(compiled.globs, pattern)
		} else {
			compiled.exact[pattern] = true
		}
	}
	return compiled, nil
}

func (patterns prunePatterns) matches(name string) bool {
	if patterns.exact[name] {
		return true
	}
	for _, p := range patterns.globs {
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}

// scanRoot uses File.ReadDir rather than filepath.WalkDir because final
// repository ordering is established separately. Avoiding a lexical sort in
// every directory materially reduces traversal work in large trees.
type scanDirectory struct {
	path  string
	depth int
}

func scanRoot(root string, hidden bool, patterns prunePatterns, maxDepth *int, seen map[string]bool, repos *[]string, stats *scanStats, progress ProgressFunc) error {
	pending := []scanDirectory{{path: root}}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		entries, err := readDirectory(current.path)
		if err != nil {
			if current.path == root {
				return err
			}
			stats.unreadable++
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			entryPath := filepath.Join(current.path, name)
			stats.entries++
			isDir := entry.IsDir()
			if isDir {
				stats.directories++
				reportDiscovery(progress, entryPath, *stats, len(*repos), false)
			}
			if name == ".git" {
				if absolute, absErr := filepath.Abs(current.path); absErr == nil && !seen[absolute] {
					seen[absolute] = true
					*repos = append(*repos, current.path)
					reportDiscovery(progress, current.path, *stats, len(*repos), true)
				}
				continue
			}
			if !isDir {
				continue
			}
			if maxDepth != nil && current.depth >= *maxDepth {
				stats.depthSkipped++
				continue
			}
			if patterns.matches(name) {
				stats.pruned++
				continue
			}
			if !hidden && (strings.HasPrefix(name, ".") || nativeHidden(entryPath, entry)) {
				stats.hiddenSkipped++
				continue
			}
			pending = append(pending, scanDirectory{path: entryPath, depth: current.depth + 1})
		}
	}
	return nil
}

func readDirectory(dir string) ([]fs.DirEntry, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	entries, readErr := f.ReadDir(-1)
	closeErr := f.Close()
	if readErr != nil {
		return nil, readErr
	}
	return entries, closeErr
}

const discoveryProgressInterval = 32

func reportDiscovery(progress ProgressFunc, path string, stats scanStats, repositories int, force bool) {
	if progress == nil || (!force && stats.directories%discoveryProgressInterval != 0) {
		return
	}
	reportProgress(progress, ProgressEvent{
		Kind: ProgressDiscovery, Path: path, Entries: stats.entries,
		Directories: stats.directories, Repositories: repositories,
	})
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
