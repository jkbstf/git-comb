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
	// Empty means the repository has no commits yet.
	Empty bool
	// Ahead means the current branch is ahead of its upstream.
	Ahead bool
	// Behind means the current branch is behind its upstream, as of
	// the last fetch.
	Behind bool
	// Unpushed counts commits reachable from HEAD or any local branch
	// but from no remote-tracking ref.
	Unpushed int
	// Stashes counts stash entries.
	Stashes int
	// NoRemote means no remote is configured at all.
	NoRemote bool
	// FetchFailed means --fetch could not reach some remote.
	FetchFailed bool
	// Linked marks a linked worktree. Ref-store state (unpushed
	// commits, stashes) is shared with the primary worktree and is
	// reported there once.
	Linked bool
	// UnpushedBranches carries per-branch unpushed counts when the
	// probe ran verbose.
	UnpushedBranches []BranchCount
	// Err is the probe failure, if any; other fields are then zero.
	Err error
}

// BranchCount pairs a branch name with its unpushed commit count.
type BranchCount struct {
	Name    string
	Commits int
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
		b = append(b, 'N')
	}
	if r.FetchFailed {
		b = append(b, 'R')
	}
	return string(b)
}

// Clean reports whether nothing needs attention.
func (r Report) Clean() bool { return r.Err == nil && r.Signs() == "" }
