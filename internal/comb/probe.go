package comb

import (
	"fmt"
	"path"
	"strings"
	"sync"
	"time"
)

// terminalMu keeps interactive authentication usable while probes and
// incremental result rendering proceed concurrently. Local probes remain
// parallel, but a fetch prompt and durable output never own the terminal at
// the same time.
var terminalMu sync.Mutex

// WithTerminal runs a short durable-output operation without colliding with
// an interactive fetch prompt.
func WithTerminal(fn func()) {
	terminalMu.Lock()
	defer terminalMu.Unlock()
	fn()
}

func fetch(git gitRunner, repo string) error {
	var waiting time.Time
	if git.diagnostics != nil {
		waiting = time.Now()
	}
	terminalMu.Lock()
	if git.diagnostics != nil {
		git.diagnostics.Wait(repo, "fetch", waiting)
	}
	defer terminalMu.Unlock()
	_, err := git.out(repo, "fetch", "fetch", "--all", "--quiet")
	return err
}

// probe interrogates one repository. Every command is read-only; the
// single exception is the opt-in fetch, run once per repository group
// by its carrier. Fetches may prompt for credentials, so they are
// serialized while the remaining local probes stay parallel.
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
func probe(git gitRunner, repo string, opts Options, carrier, linked bool) Report {
	r := Report{Path: repo, Linked: linked}
	needUnpushed := opts.Only.Has('U')
	needAhead := opts.Only.Has('A')
	needBehind := opts.Only.Has('B')
	needSync := needAhead || needBehind
	needStash := opts.Only.Has('S')
	needEmpty := opts.Only.Has('E')
	needRemotes := needUnpushed || opts.Only.Has('L')

	var ackGlobs []string
	remoteKnown := false
	if !opts.NoIgnores {
		ignored, globs, configuredRemote, err := repoSettings(git, repo, needRemotes)
		if err != nil {
			r.Err = err
			return r
		}
		if ignored {
			r.Ignored = true
			return r
		}
		ackGlobs = globs
		remoteKnown = configuredRemote
	}

	if opts.Fetch && carrier {
		if err := fetch(git, repo); err != nil {
			r.FetchFailed = true
		}
	}

	// The status probe always runs: it is the repository sanity check
	// and the source of the checked-out branch, dirt, and HEAD state.
	// Enumerating untracked files is the expensive part, so it is
	// skipped when D was not asked for. Ahead/behind calculation and
	// rename classification do not feed the status parser.
	untracked := "-uno"
	if opts.Only.Has('D') {
		untracked = "-unormal"
		if opts.DirtyDetails {
			untracked = "-uall"
		}
	}
	out, err := git.out(repo, "status", "status", "--porcelain=v2", "--branch", "--no-ahead-behind", "--no-renames", untracked)
	if err != nil {
		r.Err = err
		return r
	}
	st := parseStatus(out)
	r.Dirty = st.Dirty
	r.Branch = describeBranch(st)
	if opts.DirtyDetails && r.Dirty {
		if !st.TrackedDirty {
			r.DirtyStat = ShortStat{Untracked: st.Untracked}
		} else {
			// Detail is presentational: an unusual diff failure must not
			// hide an otherwise valid dirty finding.
			if stat, err := worktreeShortStat(git, repo, st); err == nil {
				r.DirtyStat = stat
			}
		}
	}

	if needRemotes {
		if remoteKnown {
			r.NoRemote = false
		} else {
			// A merged remote.* config entry proves a normal configured
			// remote exists. Fall back to git remote when it does not, so
			// legacy or otherwise unusual remote layouts retain Git's exact
			// semantics.
			remotes, err := git.out(repo, "remotes", "remote")
			if err != nil {
				r.Err = err
				return r
			}
			r.NoRemote = strings.TrimSpace(remotes) == ""
		}
	}

	// An unborn HEAD is not the same as an empty repository: after an
	// orphan checkout, other branches can still hold unpushed work.
	var branches []branchRef
	stashKnown, hasStash := false, false
	if (st.Unborn && (needEmpty || needUnpushed)) || (carrier && (needUnpushed || needSync)) {
		branches, hasStash, err = listBranchRefs(git, repo, needSync || opts.BranchDetails, needStash)
		if err != nil {
			r.Err = err
			return r
		}
		stashKnown = needStash
	}
	if needEmpty {
		r.Empty = st.Unborn && len(branches) == 0
	}

	// Unpushed means reachable from local refs but from no
	// remote-tracking ref — no upstream configuration needed, no
	// network touched. With no remotes at all every commit would
	// count, so the L sign carries that case instead. The carrier
	// counts branch-reachable commits once for the whole group; each
	// worktree adds its own detached-HEAD commits, restricted to
	// those on no local branch so the two never overlap.
	var kept []string
	var branchUnpushed map[string]int
	detachedUnpushed := 0
	if needUnpushed && !r.NoRemote {
		if carrier && len(branches) > 0 {
			kept = branchNames(branches)
			if len(ackGlobs) > 0 {
				var acked []string
				kept, acked, err = partitionBranches(kept, ackGlobs)
				if err != nil {
					r.Err = err
					return r
				}
				r.AckedBranches = len(acked)
			}
			if len(kept) > 0 {
				var n int
				if opts.BranchDetails {
					n, branchUnpushed, err = localOnlyBranchCounts(git, repo, branches, kept)
				} else {
					args := []string{"rev-list", "--count"}
					for _, name := range kept {
						args = append(args, "refs/heads/"+name)
					}
					args = append(args, "--not", "--remotes")
					n, err = git.count(repo, "unpushed_aggregate", args...)
				}
				if err != nil {
					r.Err = err
					return r
				}
				r.Unpushed += n
				if n == 0 {
					// No individual branch can have local-only history when
					// their aggregate union is empty.
					kept = nil
				}
			}
		}
		if st.Detached {
			n, err := git.count(repo, "detached_unpushed", "rev-list", "--count", "HEAD", "--not", "--remotes", "--branches")
			if err != nil {
				r.Err = err
				return r
			}
			detachedUnpushed = n
			r.Unpushed += n
		}
	}

	if carrier && needStash && (!stashKnown || hasStash) {
		// refs/stash does not exist when there are no stashes; that
		// error simply means zero.
		if n, err := git.count(repo, "stash", "rev-list", "--walk-reflogs", "--count", "refs/stash"); err == nil {
			r.Stashes = n
		}
	}

	if carrier && (needUnpushed || needSync) {
		states, ahead, behind := inspectBranches(branches, kept, branchUnpushed, opts)
		r.Branches, r.Ahead, r.Behind = states, ahead, behind
	}
	if opts.BranchDetails && st.Detached && detachedUnpushed > 0 {
		// A detached HEAD is worktree-local and therefore cannot be
		// represented by the carrier's local-branch enumeration.
		r.Branches = append(r.Branches, BranchStatus{
			Name: "(detached HEAD)", Unpushed: detachedUnpushed, InWorktree: true, Detached: true,
		})
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
