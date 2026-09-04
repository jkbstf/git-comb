package comb

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"slices"
	"sync"
	"time"
)

const diagnosticSchema = 2

var diagnosticOperations = map[string]bool{
	"branches": true, "classify_worktree": true, "config": true,
	"detached_unpushed": true, "dirty_diff": true, "divergence": true,
	"empty_tree": true, "fetch": true, "remotes": true, "stash": true,
	"status": true, "unpushed_aggregate": true, "unpushed_branch": true,
	"unpushed_graph": true,
}

var diagnosticPhases = map[string]bool{
	"config": true, "discovery": true, "grouping": true,
	"probing": true, "rendering": true,
}

var diagnosticCountNames = map[string]bool{
	"directories": true, "entries": true, "groups": true,
	"hidden_skipped": true, "depth_skipped": true, "linked_worktrees": true, "pruned": true,
	"repositories": true, "unreadable": true,
}

var diagnosticResources = map[string]bool{"fetch": true}
var diagnosticResults = map[string]bool{"ok": true, "exit": true, "start_error": true}

// Diagnostics writes privacy-safe performance events for one run. It records
// timings and anonymous operation metadata only: callers never pass paths,
// refs, arguments, command output, environment values, or error text to it.
type Diagnostics struct {
	mu      sync.Mutex
	w       io.Writer
	started time.Time
	err     error

	nextGitID      int
	repoIDs        map[string]string
	activeGit      int
	maxActiveGit   int
	activeProbes   int
	maxActiveProbe int
	gitDurations   map[string][]time.Duration
	repoGit        map[string]diagnosticRepoGit
}

type diagnosticRepoGit struct {
	count    int
	duration time.Duration
}

type diagnosticOperation struct {
	Name      string  `json:"name"`
	Count     int     `json:"count"`
	TotalMS   float64 `json:"total_ms"`
	MedianMS  float64 `json:"median_ms"`
	P95MS     float64 `json:"p95_ms"`
	MaximumMS float64 `json:"maximum_ms"`
}

// DiagnosticOptions is the privacy-safe effective configuration included in
// a diagnostic report. It deliberately contains no option values that can
// identify repositories, branches, remotes, users, or machines.
type DiagnosticOptions struct {
	Roots     int    `json:"roots"`
	Jobs      int    `json:"jobs"`
	Prunes    int    `json:"prunes"`
	Selection string `json:"selection"`
	Short     bool   `json:"short"`
	All       bool   `json:"all"`
	Fetch     bool   `json:"fetch"`
	Hidden    bool   `json:"hidden"`
	NoIgnores bool   `json:"no_ignores"`
	MaxDepth  *int   `json:"max_depth,omitempty"`
}

// NewDiagnostics starts a versioned JSON Lines diagnostic stream.
func NewDiagnostics(w io.Writer, version string) (*Diagnostics, error) {
	d := &Diagnostics{
		w:            w,
		started:      time.Now(),
		repoIDs:      make(map[string]string),
		gitDurations: make(map[string][]time.Duration),
		repoGit:      make(map[string]diagnosticRepoGit),
	}
	err := d.writeLocked(struct {
		Schema    int     `json:"schema"`
		Event     string  `json:"event"`
		OffsetMS  float64 `json:"offset_ms"`
		Version   string  `json:"version"`
		GoVersion string  `json:"go_version"`
		OS        string  `json:"os"`
		Arch      string  `json:"arch"`
		CPUs      int     `json:"cpus"`
	}{diagnosticSchema, "run_start", 0, version, runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU()})
	if err != nil {
		return nil, err
	}
	return d, nil
}

// Options records the resolved, non-identifying options for the run.
func (d *Diagnostics) Options(opts DiagnosticOptions) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	_ = d.writeLocked(struct {
		Event    string            `json:"event"`
		OffsetMS float64           `json:"offset_ms"`
		Options  DiagnosticOptions `json:"options"`
	}{"options", d.offsetMS(time.Now()), opts})
}

// RegisterRepositories assigns run-local identifiers without emitting paths.
func (d *Diagnostics) RegisterRepositories(repos []string) {
	if d == nil {
		return
	}
	ordered := slices.Clone(repos)
	slices.Sort(ordered)
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, repo := range ordered {
		if _, exists := d.repoIDs[repo]; !exists {
			d.repoIDs[repo] = "repo-" + threeDigits(len(d.repoIDs)+1)
		}
	}
}

// Phase records one completed run phase and non-identifying counters.
func (d *Diagnostics) Phase(name string, started time.Time, counts map[string]int) {
	d.PhaseDuration(name, time.Since(started), counts)
}

// PhaseDuration records a phase whose work was accumulated across several
// intervals, such as incremental rendering between repository completions.
func (d *Diagnostics) PhaseDuration(name string, duration time.Duration, counts map[string]int) {
	if d == nil {
		return
	}
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	_ = d.writeLocked(struct {
		Event      string         `json:"event"`
		OffsetMS   float64        `json:"offset_ms"`
		Phase      string         `json:"phase"`
		DurationMS float64        `json:"duration_ms"`
		Counts     map[string]int `json:"counts,omitempty"`
	}{"phase_end", d.offsetMS(now), safeDiagnosticValue(name, diagnosticPhases), milliseconds(duration), safeDiagnosticCounts(counts)})
}

// Wait records time spent waiting for a serialized resource such as the fetch
// terminal mutex.
func (d *Diagnostics) Wait(repo, resource string, started time.Time) {
	if d == nil {
		return
	}
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	_ = d.writeLocked(struct {
		Event      string  `json:"event"`
		OffsetMS   float64 `json:"offset_ms"`
		Repository string  `json:"repository,omitempty"`
		Resource   string  `json:"resource"`
		DurationMS float64 `json:"duration_ms"`
	}{"wait_end", d.offsetMS(now), d.repoIDLocked(repo), safeDiagnosticValue(resource, diagnosticResources), milliseconds(now.Sub(started))})
}

// RepositoryStart records queue time and returns the probe start instant.
func (d *Diagnostics) RepositoryStart(repo string, queued time.Time) time.Time {
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.activeProbes++
	d.maxActiveProbe = max(d.maxActiveProbe, d.activeProbes)
	_ = d.writeLocked(struct {
		Event      string  `json:"event"`
		OffsetMS   float64 `json:"offset_ms"`
		Repository string  `json:"repository"`
		QueueMS    float64 `json:"queue_ms"`
	}{"repository_start", d.offsetMS(now), d.repoIDLocked(repo), milliseconds(now.Sub(queued))})
	return now
}

// RepositoryEnd records aggregate timing for one anonymous repository.
func (d *Diagnostics) RepositoryEnd(repo string, started time.Time) {
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.activeProbes--
	id := d.repoIDLocked(repo)
	git := d.repoGit[id]
	_ = d.writeLocked(struct {
		Event         string  `json:"event"`
		OffsetMS      float64 `json:"offset_ms"`
		Repository    string  `json:"repository"`
		DurationMS    float64 `json:"duration_ms"`
		GitProcesses  int     `json:"git_processes"`
		GitDurationMS float64 `json:"git_duration_ms"`
	}{"repository_end", d.offsetMS(now), id, milliseconds(now.Sub(started)), git.count, milliseconds(git.duration)})
}

type diagnosticGitSpan struct {
	d         *Diagnostics
	id        int
	repo      string
	operation string
	started   time.Time
}

func (d *Diagnostics) startGit(repo, operation string) *diagnosticGitSpan {
	if d == nil {
		return nil
	}
	now := time.Now()
	operation = safeDiagnosticValue(operation, diagnosticOperations)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.nextGitID++
	d.activeGit++
	d.maxActiveGit = max(d.maxActiveGit, d.activeGit)
	span := &diagnosticGitSpan{d: d, id: d.nextGitID, repo: repo, operation: operation, started: now}
	_ = d.writeLocked(struct {
		Event      string  `json:"event"`
		OffsetMS   float64 `json:"offset_ms"`
		Invocation int     `json:"invocation"`
		Repository string  `json:"repository,omitempty"`
		Operation  string  `json:"operation"`
	}{"git_start", d.offsetMS(now), span.id, d.repoIDLocked(repo), operation})
	return span
}

func (s *diagnosticGitSpan) end(result string, exitCode int) {
	if s == nil {
		return
	}
	now := time.Now()
	result = safeDiagnosticValue(result, diagnosticResults)
	duration := now.Sub(s.started)
	d := s.d
	d.mu.Lock()
	defer d.mu.Unlock()
	d.activeGit--
	d.gitDurations[s.operation] = append(d.gitDurations[s.operation], duration)
	repoID := d.repoIDLocked(s.repo)
	if repoID != "" {
		stats := d.repoGit[repoID]
		stats.count++
		stats.duration += duration
		d.repoGit[repoID] = stats
	}
	_ = d.writeLocked(struct {
		Event      string  `json:"event"`
		OffsetMS   float64 `json:"offset_ms"`
		Invocation int     `json:"invocation"`
		Repository string  `json:"repository,omitempty"`
		Operation  string  `json:"operation"`
		DurationMS float64 `json:"duration_ms"`
		Result     string  `json:"result"`
		ExitCode   int     `json:"exit_code"`
	}{"git_end", d.offsetMS(now), s.id, repoID, s.operation, milliseconds(duration), result, exitCode})
}

// Finish closes the logical diagnostic stream and returns the first write
// failure, if any.
func (d *Diagnostics) Finish(exitCode int) error {
	if d == nil {
		return nil
	}
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	operations := make([]diagnosticOperation, 0, len(d.gitDurations))
	for name, samples := range d.gitDurations {
		slices.Sort(samples)
		var total time.Duration
		for _, sample := range samples {
			total += sample
		}
		operations = append(operations, diagnosticOperation{
			Name: name, Count: len(samples), TotalMS: milliseconds(total),
			MedianMS:  milliseconds(percentile(samples, 50)),
			P95MS:     milliseconds(percentile(samples, 95)),
			MaximumMS: milliseconds(samples[len(samples)-1]),
		})
	}
	slices.SortFunc(operations, func(a, b diagnosticOperation) int { return cmp.Compare(a.Name, b.Name) })
	_ = d.writeLocked(struct {
		Event           string                `json:"event"`
		OffsetMS        float64               `json:"offset_ms"`
		DurationMS      float64               `json:"duration_ms"`
		ExitCode        int                   `json:"exit_code"`
		GitProcesses    int                   `json:"git_processes"`
		MaxActiveGit    int                   `json:"max_active_git"`
		MaxActiveProbes int                   `json:"max_active_probes"`
		Operations      []diagnosticOperation `json:"operations"`
	}{"run_end", d.offsetMS(now), milliseconds(now.Sub(d.started)), exitCode, d.nextGitID, d.maxActiveGit, d.maxActiveProbe, operations})
	return d.err
}

// Err returns the first diagnostic write failure.
func (d *Diagnostics) Err() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
}

func (d *Diagnostics) writeLocked(event any) error {
	if d.err != nil {
		return d.err
	}
	line, err := json.Marshal(event)
	if err == nil {
		line = append(line, '\n')
		var n int
		n, err = d.w.Write(line)
		if err == nil && n != len(line) {
			err = io.ErrShortWrite
		}
	}
	if err != nil {
		d.err = err
	}
	return err
}

func (d *Diagnostics) repoIDLocked(repo string) string {
	if repo == "" {
		return ""
	}
	return d.repoIDs[repo]
}

func (d *Diagnostics) offsetMS(t time.Time) float64 {
	return milliseconds(t.Sub(d.started))
}

func milliseconds(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

func percentile(sorted []time.Duration, percent int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	index := (len(sorted)*percent + 99) / 100
	return sorted[index-1]
}

func threeDigits(n int) string {
	return fmt.Sprintf("%03d", n)
}

func safeDiagnosticValue(value string, allowed map[string]bool) string {
	if allowed[value] {
		return value
	}
	return "unknown"
}

func safeDiagnosticCounts(counts map[string]int) map[string]int {
	if counts == nil {
		return nil
	}
	safe := make(map[string]int, len(counts))
	for name, count := range counts {
		if diagnosticCountNames[name] {
			safe[name] = count
		}
	}
	return safe
}
