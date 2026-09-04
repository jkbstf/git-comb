// Package comb finds Git repositories holding work that exists only
// on the local machine: uncommitted changes, commits unreachable from
// any remote, and stashes. Every question is answered by the system
// git binary through its documented stable interfaces, so repository
// format changes ship for free with git itself.
package comb

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// Options configure a Run.
type Options struct {
	// Roots are the directories to comb.
	Roots []string
	// Fetch updates all remotes before probing, prompting for
	// authentication when needed, so behind is current.
	Fetch bool
	// BranchDetails gathers per-branch unpushed and upstream divergence
	// counts for the detailed view. The short view leaves this false to
	// keep probing lighter.
	BranchDetails bool
	// DirtyDetails gathers a diff-style summary for the detailed view.
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
	// Diagnostics records privacy-safe performance metadata. Nil keeps
	// diagnostics fully disabled.
	Diagnostics *Diagnostics
	// Progress receives transient, local presentation events. Events may carry
	// paths and are deliberately separate from privacy-safe diagnostics.
	Progress ProgressFunc
	// Report receives each completed report in deterministic path order.
	Report func(Report)
}

// ProgressKind identifies a presentation event emitted while a run is active.
type ProgressKind uint8

const (
	// ProgressPhase announces a transition between scanning, preparing,
	// checking, and completion.
	ProgressPhase ProgressKind = iota
	// ProgressDiscovery carries filesystem traversal counters.
	ProgressDiscovery
	// ProgressRepositoryStart marks a repository entering the probe pool.
	ProgressRepositoryStart
	// ProgressRepositoryEnd carries the completed repository outcome.
	ProgressRepositoryEnd
	// ProgressGitStart marks a child Git operation becoming active.
	ProgressGitStart
	// ProgressGitEnd marks a child Git operation completing.
	ProgressGitEnd
)

// ProgressEvent is an ephemeral snapshot for interactive presentation. It is
// not part of the diagnostic format and may contain local paths.
type ProgressEvent struct {
	Kind                   ProgressKind
	Phase, Path, Operation string
	Entries, Directories   int
	Repositories           int
	Total                  int
	Attention, Failed      bool
}

// ProgressFunc consumes a progress event. Implementations must be safe for
// concurrent calls from repository and Git workers.
type ProgressFunc func(ProgressEvent)

func reportProgress(progress ProgressFunc, event ProgressEvent) {
	if progress != nil {
		progress(event)
	}
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
	var started time.Time
	if opts.Diagnostics != nil {
		started = time.Now()
	}
	reportProgress(opts.Progress, ProgressEvent{Kind: ProgressPhase, Phase: "scanning"})
	repos, stats, err := scan(opts.Roots, opts.Hidden, opts.Prune, opts.Progress)
	if opts.Diagnostics != nil {
		opts.Diagnostics.Phase("discovery", started, map[string]int{
			"entries": stats.entries, "directories": stats.directories,
			"hidden_skipped": stats.hiddenSkipped, "pruned": stats.pruned,
			"unreadable": stats.unreadable, "repositories": len(repos),
		})
	}
	if err != nil {
		return nil, err
	}
	slices.Sort(repos)
	opts.Diagnostics.RegisterRepositories(repos)
	reportProgress(opts.Progress, ProgressEvent{Kind: ProgressPhase, Phase: "preparing", Total: len(repos)})
	reports := probeAll(repos, opts)
	reportProgress(opts.Progress, ProgressEvent{Kind: ProgressPhase, Phase: "complete", Total: len(repos)})
	return reports, nil
}

// probeAll elects one carrier per repository group, then probes
// everything concurrently. Each goroutine writes only its own slot,
// so no aggregation lock is needed. Grouping exists for the sake of
// shared-state counting and once-per-repository fetching, so when the
// run needs none of those every repository simply stands alone.
func probeAll(repos []string, opts Options) []Report {
	git := gitRunner{diagnostics: opts.Diagnostics, progress: opts.Progress}
	jobs := opts.Jobs
	if jobs < 1 {
		jobs = 1
	}
	var carriers, linked []bool
	var phaseStarted time.Time
	if opts.Diagnostics != nil {
		phaseStarted = time.Now()
	}
	if opts.Fetch || opts.Only.Has('U') || opts.Only.Has('A') || opts.Only.Has('B') || opts.Only.Has('S') {
		carriers, linked = electCarriers(git, repos, jobs)
	} else {
		carriers = make([]bool, len(repos))
		linked = make([]bool, len(repos))
		for i := range carriers {
			carriers[i] = true
		}
	}
	if opts.Diagnostics != nil {
		groups, linkedWorktrees := 0, 0
		for i := range carriers {
			if carriers[i] {
				groups++
			}
			if linked[i] {
				linkedWorktrees++
			}
		}
		opts.Diagnostics.Phase("grouping", phaseStarted, map[string]int{
			"groups": groups, "linked_worktrees": linkedWorktrees,
			"repositories": len(repos),
		})
		phaseStarted = time.Now()
	}
	reportProgress(opts.Progress, ProgressEvent{Kind: ProgressPhase, Phase: "checking", Total: len(repos)})
	sem := make(chan struct{}, jobs)
	reports := make([]Report, len(repos))
	type completedReport struct {
		index  int
		report Report
	}
	completed := make(chan completedReport, len(repos))
	var wg sync.WaitGroup
	for i, repo := range repos {
		wg.Add(1)
		go func(i int, repo string) {
			defer wg.Done()
			var queued time.Time
			if opts.Diagnostics != nil {
				queued = time.Now()
			}
			sem <- struct{}{}
			defer func() { <-sem }()
			reportProgress(opts.Progress, ProgressEvent{Kind: ProgressRepositoryStart, Path: repo, Total: len(repos)})
			var started time.Time
			if opts.Diagnostics != nil {
				started = opts.Diagnostics.RepositoryStart(repo, queued)
				defer opts.Diagnostics.RepositoryEnd(repo, started)
			}
			report := probe(git, repo, opts, carriers[i], linked[i])
			reportProgress(opts.Progress, ProgressEvent{
				Kind: ProgressRepositoryEnd, Path: repo, Total: len(repos),
				Attention: !report.Ignored && report.Err == nil && opts.Only.Filter(report.Signs()) != "",
				Failed:    !report.Ignored && report.Err != nil,
			})
			completed <- completedReport{index: i, report: report}
		}(i, repo)
	}
	go func() {
		wg.Wait()
		close(completed)
	}()
	pending := make(map[int]Report)
	next := 0
	for item := range completed {
		pending[item.index] = item.report
		for {
			report, ok := pending[next]
			if !ok {
				break
			}
			delete(pending, next)
			reports[next] = report
			if opts.Report != nil {
				opts.Report(report)
			}
			next++
		}
	}
	if opts.Diagnostics != nil {
		opts.Diagnostics.Phase("probing", phaseStarted, map[string]int{"repositories": len(repos)})
	}
	return reports
}

// electCarriers groups the discovered worktrees by the common git dir
// they share and marks one carrier per group to count shared state:
// the primary worktree when it was discovered, otherwise the
// first-sorted linked worktree — a linked worktree scanned without
// its primary must still report the repository's unpushed work. A
// worktree that cannot be classified forms its own group so its probe
// surfaces the error.
func electCarriers(git gitRunner, repos []string, jobs int) (carriers, linked []bool) {
	type location struct {
		gitDir, commonDir string
		ok                bool
	}
	locs := make([]location, len(repos))

	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup
	for i, repo := range repos {
		gitDir := filepath.Join(repo, ".git")
		info, err := os.Lstat(gitDir)
		if err == nil && info.IsDir() {
			_, commonErr := os.Lstat(filepath.Join(gitDir, "commondir"))
			if errors.Is(commonErr, os.ErrNotExist) {
				if absolute, absErr := filepath.Abs(gitDir); absErr == nil {
					absolute = filepath.Clean(absolute)
					locs[i] = location{gitDir: absolute, commonDir: absolute, ok: true}
					continue
				}
			}
		}
		wg.Add(1)
		go func(i int, repo string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out, err := git.out(repo, "classify_worktree", "rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir")
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
