// Package comb finds Git repositories holding work that exists only
// on the local machine: uncommitted changes, commits unreachable from
// any remote, and stashes. Every question is answered by the system
// git binary through its documented stable interfaces, so repository
// format changes ship for free with git itself.
package comb

import (
	"runtime"
	"sort"
	"strings"
	"sync"
)

// Options configure a Run.
type Options struct {
	// Roots are the directories to comb.
	Roots []string
	// Fetch updates all remotes before probing, so behind is current.
	Fetch bool
	// Verbose gathers per-branch unpushed detail.
	Verbose bool
	// All keeps clean repositories in the rendered output.
	All bool
	// Hidden descends into hidden directories during discovery.
	Hidden bool
	// Jobs bounds how many repositories are probed concurrently.
	Jobs int
	// Prune lists directory names never descended into.
	Prune PruneList
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
	sort.Slice(reports, func(i, j int) bool { return reports[i].Path < reports[j].Path })
	return reports, nil
}

// probeAll probes repositories concurrently. Each goroutine writes
// only its own slot, so no aggregation lock is needed.
func probeAll(repos []string, opts Options) []Report {
	jobs := opts.Jobs
	if jobs < 1 {
		jobs = 1
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
			reports[i] = probe(repo, opts)
		}(i, repo)
	}
	wg.Wait()
	return reports
}
