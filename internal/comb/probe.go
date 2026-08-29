package comb

import (
	"path/filepath"
	"strings"
)

// probe interrogates one repository. Every command is read-only; the
// single exception is the opt-in fetch, which updates remote-tracking
// refs so behind means something.
func probe(repo string, opts Options) Report {
	r := Report{Path: repo}

	if opts.Fetch {
		if _, err := gitOut(repo, "fetch", "--all", "--quiet"); err != nil {
			r.FetchFailed = true
		}
	}

	out, err := gitOut(repo, "status", "--porcelain=v2", "--branch", "-uall")
	if err != nil {
		r.Err = err
		return r
	}
	st := parseStatus(out)
	r.Dirty = st.Dirty
	r.Empty = st.Empty
	r.Ahead = st.Ahead > 0
	r.Behind = st.Behind > 0
	r.Branch = describeBranch(st)

	remotes, err := gitOut(repo, "remote")
	if err != nil {
		r.Err = err
		return r
	}
	r.NoRemote = strings.TrimSpace(remotes) == ""

	r.Linked = linkedWorktree(repo)

	// Unpushed means reachable from HEAD or any local branch but from
	// no remote-tracking ref. This needs no upstream configuration, so
	// branches that were never pushed are found, and it never touches
	// the network. With no remotes at all every commit would count, so
	// the N sign carries that case instead. Linked worktrees share the
	// ref store with their primary; the primary counts it once.
	if !r.Empty && !r.NoRemote && !r.Linked {
		n, err := gitCount(repo, "rev-list", "--count", "HEAD", "--branches", "--not", "--remotes")
		if err != nil {
			r.Err = err
			return r
		}
		r.Unpushed = n
	}

	if !r.Linked {
		// refs/stash does not exist when there are no stashes; that
		// error simply means zero.
		if n, err := gitCount(repo, "rev-list", "--walk-reflogs", "--count", "refs/stash"); err == nil {
			r.Stashes = n
		}
	}

	if opts.Verbose && r.Unpushed > 0 {
		r.UnpushedBranches = unpushedBranches(repo, st.Detached)
	}
	return r
}

// describeBranch names what is checked out: the branch, or the
// shortened detached-HEAD object id.
func describeBranch(st worktreeStatus) string {
	if !st.Detached {
		return st.Branch
	}
	if len(st.OID) >= 7 {
		return "detached@" + st.OID[:7]
	}
	return "detached"
}

// linkedWorktree reports whether repo is a linked worktree rather
// than the primary one: its git dir then differs from the common dir
// shared by all worktrees of the repository.
func linkedWorktree(repo string) bool {
	out, err := gitOut(repo, "rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir")
	if err != nil {
		return false
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		return false
	}
	return filepath.Clean(lines[0]) != filepath.Clean(lines[1])
}

// unpushedBranches lists every local branch whose history is not
// fully on some remote, with the count of missing commits. A detached
// HEAD holding such commits is listed as "(detached)".
func unpushedBranches(repo string, detached bool) []BranchCount {
	out, err := gitOut(repo, "for-each-ref", "refs/heads", "--format=%(refname:short)")
	if err != nil {
		return nil
	}
	var res []BranchCount
	for _, name := range strings.Fields(out) {
		n, err := gitCount(repo, "rev-list", "--count", name, "--not", "--remotes")
		if err == nil && n > 0 {
			res = append(res, BranchCount{Name: name, Commits: n})
		}
	}
	if detached {
		if n, err := gitCount(repo, "rev-list", "--count", "HEAD", "--not", "--remotes"); err == nil && n > 0 {
			res = append(res, BranchCount{Name: "(detached)", Commits: n})
		}
	}
	return res
}
