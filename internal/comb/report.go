package comb

// Report describes one repository's local-only state.
type Report struct {
	// Path is the repository worktree root as discovered.
	Path string
	// Branch names the checked-out branch, or "detached@<oid>" with a
	// shortened object id on a detached HEAD.
	Branch string
	// Dirty means uncommitted changes, untracked files included.
	Dirty bool
	// DirtyStat summarizes tracked changes relative to HEAD and counts
	// untracked files when the probe gathered dirty details.
	DirtyStat ShortStat
	// Empty means the repository has no commits on any branch.
	Empty bool
	// Ahead means at least one local branch is ahead of its upstream.
	Ahead bool
	// Behind means at least one local branch is behind its upstream, as
	// of the last fetch.
	Behind bool
	// Unpushed counts commits reachable from local refs but from no
	// remote-tracking ref. The carrier of a worktree group counts the
	// branches once; every worktree adds its own detached-HEAD
	// commits.
	Unpushed int
	// Stashes counts stash entries, reported by the group's carrier.
	Stashes int
	// NoRemote means no remote is configured at all.
	NoRemote bool
	// FetchFailed means --fetch could not reach some remote.
	FetchFailed bool
	// Linked marks a linked worktree, one sharing its ref store with
	// a primary worktree elsewhere.
	Linked bool
	// Ignored marks a repository acknowledged by comb.ignore; it was
	// not probed and is disclosed only through the summary count.
	Ignored bool
	// AckedBranches counts branch names whose unpushed commits were
	// acknowledged by comb.ignoreBranch globs on this probe.
	AckedBranches int
	// Branches carries one combined status per branch needing attention
	// when the probe gathered branch details.
	Branches []BranchStatus
	// Err is the probe failure, if any; other fields are then zero.
	Err error
}

// BranchStatus combines the independent questions asked about one
// branch: whether its commits exist on any remote and how it relates
// to its configured upstream.
type BranchStatus struct {
	Name     string
	Upstream string
	Unpushed int
	Ahead    int
	Behind   int
	// InWorktree means the branch is checked out in this repository's
	// current worktree or in one of its linked worktrees.
	InWorktree bool
	Detached   bool
	// UpstreamGone means the branch is configured to track Upstream,
	// but that remote-tracking ref no longer exists locally.
	UpstreamGone bool
}

// ShortStat is the diff --shortstat-style summary of a working tree.
// FilesChanged counts tracked paths; Untracked is kept separate
// because Git diffs do not include them.
type ShortStat struct {
	FilesChanged int
	Insertions   int
	Deletions    int
	Untracked    int
}

// Signs renders the findings column: single characters in a fixed
// order, so columns line up and stay grep-friendly.
func (r Report) Signs() string {
	var b []byte
	if r.Dirty {
		b = append(b, 'D')
	}
	if r.Unpushed > 0 {
		b = append(b, 'U')
	}
	if r.Ahead {
		b = append(b, 'A')
	}
	if r.Behind {
		b = append(b, 'B')
	}
	if r.Stashes > 0 {
		b = append(b, 'S')
	}
	if r.Empty {
		b = append(b, 'E')
	}
	if r.NoRemote {
		b = append(b, 'L')
	}
	if r.FetchFailed {
		b = append(b, 'O')
	}
	return string(b)
}
