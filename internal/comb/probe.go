package comb

import (
	"fmt"
	"path"
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

	var ackGlobs []string
	if !opts.NoIgnores {
		ignored, globs, err := repoIgnores(repo)
		if err != nil {
			r.Err = err
			return r
		}
		if ignored {
			r.Ignored = true
			return r
		}
		ackGlobs = globs
	}

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
	var branches []string
	if (st.Unborn && (needEmpty || needUnpushed)) || (carrier && needUnpushed && !r.NoRemote) {
		branches, err = listBranches(repo)
		if err != nil {
			r.Err = err
			return r
		}
	}
	if needEmpty {
		r.Empty = st.Unborn && len(branches) == 0
	}

	// Unpushed means reachable from local refs but from no
	// remote-tracking ref — no upstream configuration needed, no
	// network touched. With no remotes at all every commit would
	// count, so the N sign carries that case instead. The carrier
	// counts branch-reachable commits once for the whole group; each
	// worktree adds its own detached-HEAD commits, restricted to
	// those on no local branch so the two never overlap.
	var kept []string
	if needUnpushed && !r.NoRemote {
		if carrier && len(branches) > 0 {
			kept = branches
			if len(ackGlobs) > 0 {
				var acked []string
				kept, acked, err = partitionBranches(branches, ackGlobs)
				if err != nil {
					r.Err = err
					return r
				}
				r.AckedBranches = len(acked)
			}
			if len(kept) > 0 {
				args := []string{"rev-list", "--count"}
				for _, name := range kept {
					args = append(args, "refs/heads/"+name)
				}
				args = append(args, "--not", "--remotes")
				n, err := gitCount(repo, args...)
				if err != nil {
					r.Err = err
					return r
				}
				r.Unpushed += n
			}
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
		r.UnpushedBranches = unpushedBranches(repo, st.Detached, kept)
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

// listBranches names every local branch.
func listBranches(repo string) ([]string, error) {
	out, err := gitOut(repo, "for-each-ref", "refs/heads", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// partitionBranches splits branch names into kept and acknowledged
// against comb.ignoreBranch globs. Patterns match the branch name;
// as in path.Match, `*` does not cross `/`.
func partitionBranches(names, globs []string) (kept, acked []string, err error) {
	for _, name := range names {
		matched := false
		for _, glob := range globs {
			ok, err := path.Match(glob, name)
			if err != nil {
				return nil, nil, fmt.Errorf("comb.ignoreBranch: bad pattern %q", glob)
			}
			if ok {
				matched = true
				break
			}
		}
		if matched {
			acked = append(acked, name)
		} else {
			kept = append(kept, name)
		}
	}
	return kept, acked, nil
}

// unpushedBranches lists where the unpushed commits sit: one
// rev-list per kept branch — which is why verbose mode costs more on
// branch-heavy repositories — plus the worktree's own detached HEAD
// as "(detached)". The kept list is the same one the count used, so
// the detail always agrees with the total.
func unpushedBranches(repo string, detached bool, kept []string) []BranchCount {
	var res []BranchCount
	for _, name := range kept {
		n, err := gitCount(repo, "rev-list", "--count", "refs/heads/"+name, "--not", "--remotes")
		if err == nil && n > 0 {
			res = append(res, BranchCount{Name: name, Commits: n})
		}
	}
	if detached {
		if n, err := gitCount(repo, "rev-list", "--count", "HEAD", "--not", "--remotes", "--branches"); err == nil && n > 0 {
			res = append(res, BranchCount{Name: "(detached)", Commits: n})
		}
	}
	return res
}
