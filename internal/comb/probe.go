package comb

import (
	"strings"
)

// probe interrogates one repository. Every command is read-only; the
// single exception is the opt-in fetch, run once per repository group
// by its carrier, with terminal credential prompts disabled so a
// parallel scan can never hang waiting for input.
//
// carrier marks the one worktree of a repository group that counts
// state shared through the common ref store: commits on local
// branches and stashes. Every worktree, carrier or not, additionally
// reports its own working tree and — because another worktree's
// detached HEAD is invisible to branch enumeration — its own detached
// HEAD commits, counted disjointly from the branch count.
//
// Finding classes outside opts.Only are not probed at all, so a
// narrow scan is also a fast one.
func probe(repo string, opts Options, carrier, linked bool) Report {
	r := Report{Path: repo, Linked: linked}
	needUnpushed := opts.Only.Has('U')
	needStash := opts.Only.Has('S')
	needEmpty := opts.Only.Has('E')
	needRemotes := needUnpushed || opts.Only.Has('N')

	if opts.Fetch && carrier {
		if _, err := gitOutEnv(repo, []string{"GIT_TERMINAL_PROMPT=0"}, "fetch", "--all", "--quiet"); err != nil {
			r.FetchFailed = true
		}
	}

	// The status probe always runs: it is the repository sanity check
	// and the source of branch, dirt, ahead/behind, and HEAD state.
	// Enumerating untracked files is the expensive part, so it is
	// skipped when D was not asked for.
	untracked := "-uall"
	if !opts.Only.Has('D') {
		untracked = "-uno"
	}
	out, err := gitOut(repo, "status", "--porcelain=v2", "--branch", untracked)
	if err != nil {
		r.Err = err
		return r
	}
	st := parseStatus(out)
	r.Dirty = st.Dirty
	r.Ahead = st.Ahead > 0
	r.Behind = st.Behind > 0
	r.Branch = describeBranch(st)

	if needRemotes {
		remotes, err := gitOut(repo, "remote")
		if err != nil {
			r.Err = err
			return r
		}
		r.NoRemote = strings.TrimSpace(remotes) == ""
	}

	// An unborn HEAD is not the same as an empty repository: after an
	// orphan checkout, other branches can still hold unpushed work.
	hasBranches := false
	if (st.Unborn && (needEmpty || needUnpushed)) || (carrier && needUnpushed && !r.NoRemote) {
		hasBranches, err = repoHasBranches(repo)
		if err != nil {
			r.Err = err
			return r
		}
	}
	if needEmpty {
		r.Empty = st.Unborn && !hasBranches
	}

	// Unpushed means reachable from local refs but from no
	// remote-tracking ref — no upstream configuration needed, no
	// network touched. With no remotes at all every commit would
	// count, so the N sign carries that case instead. The carrier
	// counts branch-reachable commits once for the whole group; each
	// worktree adds its own detached-HEAD commits, restricted to
	// those on no local branch so the two never overlap.
	if needUnpushed && !r.NoRemote {
		if carrier && hasBranches {
			n, err := gitCount(repo, "rev-list", "--count", "--branches", "--not", "--remotes")
			if err != nil {
				r.Err = err
				return r
			}
			r.Unpushed += n
		}
		if st.Detached {
			n, err := gitCount(repo, "rev-list", "--count", "HEAD", "--not", "--remotes", "--branches")
			if err != nil {
				r.Err = err
				return r
			}
			r.Unpushed += n
		}
	}

	if carrier && needStash {
		// refs/stash does not exist when there are no stashes; that
		// error simply means zero.
		if n, err := gitCount(repo, "rev-list", "--walk-reflogs", "--count", "refs/stash"); err == nil {
			r.Stashes = n
		}
	}

	if opts.Verbose && r.Unpushed > 0 {
		r.UnpushedBranches = unpushedBranches(repo, st.Detached, carrier)
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

// repoHasBranches reports whether any local branch exists.
func repoHasBranches(repo string) (bool, error) {
	out, err := gitOut(repo, "for-each-ref", "--count=1", "--format=%(refname)", "refs/heads")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// unpushedBranches lists where the unpushed commits sit. The carrier
// walks every local branch — one rev-list per branch, which is why
// verbose mode costs more on branch-heavy repositories — and any
// worktree adds its own detached HEAD as "(detached)".
func unpushedBranches(repo string, detached, carrier bool) []BranchCount {
	var res []BranchCount
	if carrier {
		out, err := gitOut(repo, "for-each-ref", "refs/heads", "--format=%(refname:short)")
		if err != nil {
			return nil
		}
		for _, name := range strings.Fields(out) {
			n, err := gitCount(repo, "rev-list", "--count", name, "--not", "--remotes")
			if err == nil && n > 0 {
				res = append(res, BranchCount{Name: name, Commits: n})
			}
		}
	}
	if detached {
		if n, err := gitCount(repo, "rev-list", "--count", "HEAD", "--not", "--remotes", "--branches"); err == nil && n > 0 {
			res = append(res, BranchCount{Name: "(detached)", Commits: n})
		}
	}
	return res
}
