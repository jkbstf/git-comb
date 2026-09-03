// Package comb finds Git repositories holding work that exists only
// on the local machine: uncommitted changes, commits unreachable from
// any remote, and stashes. Every question is answered by the system
// git binary through its documented stable interfaces, so repository
// format changes ship for free with git itself.
package comb

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
)

// Options configure a Run.
type Options struct {
	// Roots are the directories to comb.
	Roots []string
	// Fetch updates all remotes before probing, prompting for
	// authentication when needed, so behind is current.
	Fetch bool
	// BranchDetails gathers per-branch unpushed and upstream divergence
	// counts for the grouped view. The short view leaves this false to
	// keep probing lighter.
	BranchDetails bool
	// DirtyDetails gathers a diff-style summary for the grouped view.
	// The short view leaves this false to keep probing lighter.
	DirtyDetails bool
	// All keeps clean repositories in the rendered output.
	All bool
	// Hidden descends into hidden directories during discovery.
	Hidden bool
	// Jobs bounds how many repositories are probed concurrently.
	Jobs int
	// Prune lists directory names never descended into.
	Prune PruneList
	// Only restricts the scan to selected finding classes; the zero
	// value selects all of them.
	Only SignSet
	// NoIgnores disregards comb.ignore and comb.ignoreBranch, showing
	// the unfiltered truth.
	NoIgnores bool
}

// PruneList collects the repeatable --prune flag values.
type PruneList []string

// String implements flag.Value.
func (p *PruneList) String() string { return strings.Join(*p, ",") }

// Set implements flag.Value by appending one directory name.
func (p *PruneList) Set(v string) error {
	*p = append(*p, v)
	return nil
}

// DefaultJobs is the default probe parallelism: the CPU count capped
// at eight, because each probe is a handful of short-lived git
// processes rather than sustained compute.
func DefaultJobs() int {
	n := runtime.NumCPU()
	if n > 8 {
		return 8
	}
	if n < 1 {
		return 1
	}
	return n
}

// Run discovers every repository under the roots and probes each one,
// returning reports sorted by path.
func Run(opts Options) ([]Report, error) {
	repos, err := Scan(opts.Roots, opts.Hidden, opts.Prune)
	if err != nil {
		return nil, err
	}
	reports := probeAll(repos, opts)
	slices.SortFunc(reports, func(a, b Report) int { return strings.Compare(a.Path, b.Path) })
	return reports, nil
}

// probeAll elects one carrier per repository group, then probes
// everything concurrently. Each goroutine writes only its own slot,
// so no aggregation lock is needed. Grouping exists for the sake of
// shared-state counting and once-per-repository fetching, so when the
// run needs none of those every repository simply stands alone.
func probeAll(repos []string, opts Options) []Report {
	jobs := opts.Jobs
	if jobs < 1 {
		jobs = 1
	}
	var carriers, linked []bool
	if opts.Fetch || opts.Only.Has('U') || opts.Only.Has('A') || opts.Only.Has('B') || opts.Only.Has('S') {
		carriers, linked = electCarriers(repos, jobs)
	} else {
		carriers = make([]bool, len(repos))
		linked = make([]bool, len(repos))
		for i := range carriers {
			carriers[i] = true
		}
	}
	sem := make(chan struct{}, jobs)
	reports := make([]Report, len(repos))
	var wg sync.WaitGroup
	for i, repo := range repos {
		wg.Add(1)
		go func(i int, repo string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			reports[i] = probe(repo, opts, carriers[i], linked[i])
		}(i, repo)
	}
	wg.Wait()
	return reports
}

// electCarriers groups the discovered worktrees by the common git dir
// they share and marks one carrier per group to count shared state:
// the primary worktree when it was discovered, otherwise the
// first-sorted linked worktree — a linked worktree scanned without
// its primary must still report the repository's unpushed work. A
// worktree that cannot be classified forms its own group so its probe
// surfaces the error.
func electCarriers(repos []string, jobs int) (carriers, linked []bool) {
	type location struct {
		gitDir, commonDir string
		ok                bool
	}
	locs := make([]location, len(repos))

	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup
	for i, repo := range repos {
		wg.Add(1)
		go func(i int, repo string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out, err := gitOut(repo, "rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir")
			if err != nil {
				return
			}
			lines := strings.Split(strings.TrimSpace(out), "\n")
			if len(lines) != 2 {
				return
			}
			locs[i] = location{
				gitDir:    filepath.Clean(lines[0]),
				commonDir: filepath.Clean(lines[1]),
				ok:        true,
			}
		}(i, repo)
	}
	wg.Wait()

	carriers = make([]bool, len(repos))
	linked = make([]bool, len(repos))
	groups := map[string][]int{}
	for i, loc := range locs {
		if !loc.ok {
			carriers[i] = true
			continue
		}
		linked[i] = loc.gitDir != loc.commonDir
		groups[loc.commonDir] = append(groups[loc.commonDir], i)
	}
	for _, idxs := range groups {
		carrier := -1
		for _, i := range idxs {
			if !linked[i] {
				carrier = i
				break
			}
		}
		if carrier < 0 {
			carrier = idxs[0]
			for _, i := range idxs[1:] {
				if repos[i] < repos[carrier] {
					carrier = i
				}
			}
		}
		carriers[carrier] = true
	}
	return carriers, linked
}
