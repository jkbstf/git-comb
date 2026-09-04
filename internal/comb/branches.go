package comb

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// branchRef is the inexpensive for-each-ref view used for every local
// branch. Track contains Git's stable trackshort marker: > ahead, <
// behind, <> diverged, and = synchronized.
type branchRef struct {
	Name, Upstream, UpstreamShort string
	Track, WorktreePath           string
}

func listBranchRefs(git gitRunner, repo string, includeTrack bool) ([]branchRef, error) {
	track := ""
	if includeTrack {
		track = "%(upstream:trackshort)"
	}
	format := "%(refname:short)%09%(upstream)%09%(upstream:short)%09" + track + "%09%(worktreepath)"
	out, err := git.out(repo, "branches", "for-each-ref", "refs/heads", "--format="+format)
	if err != nil {
		return nil, err
	}

	var refs []branchRef
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 5)
		if len(fields) != 5 {
			return nil, fmt.Errorf("malformed git for-each-ref line %q", line)
		}
		refs = append(refs, branchRef{
			Name: fields[0], Upstream: fields[1], UpstreamShort: fields[2],
			Track: fields[3], WorktreePath: fields[4],
		})
	}
	return refs, nil
}

func branchNames(refs []branchRef) []string {
	names := make([]string, len(refs))
	for i, ref := range refs {
		names[i] = ref.Name
	}
	return names
}

// inspectBranches combines global local-only work with each branch's
// upstream relationship. Per-branch counts are gathered only for the
// grouped view; the short view uses trackshort to keep this to one
// for-each-ref invocation.
func inspectBranches(git gitRunner, repo string, refs []branchRef, kept []string, opts Options) (states []BranchStatus, anyAhead, anyBehind bool, err error) {
	needUnpushed := opts.Only.Has('U')
	needAhead := opts.Only.Has('A')
	needBehind := opts.Only.Has('B')
	keptSet := make(map[string]bool, len(kept))
	for _, name := range kept {
		keptSet[name] = true
	}

	for _, ref := range refs {
		trackAhead := strings.Contains(ref.Track, ">")
		trackBehind := strings.Contains(ref.Track, "<")
		if needAhead && trackAhead {
			anyAhead = true
		}
		if needBehind && trackBehind {
			anyBehind = true
		}
		if !opts.BranchDetails {
			continue
		}

		state := BranchStatus{
			Name: ref.Name, Upstream: ref.UpstreamShort,
			InWorktree:   ref.WorktreePath != "",
			UpstreamGone: ref.Upstream != "" && ref.Track == "",
		}
		if needUnpushed && keptSet[ref.Name] {
			n, err := git.count(repo, "unpushed_branch", "rev-list", "--count", "refs/heads/"+ref.Name, "--not", "--remotes")
			if err != nil {
				return nil, false, false, err
			}
			state.Unpushed = n
		}
		if (needAhead && trackAhead) || (needBehind && trackBehind) {
			ahead, behind, err := branchDivergence(git, repo, ref)
			if err != nil {
				return nil, false, false, err
			}
			if needAhead {
				state.Ahead = ahead
			}
			if needBehind {
				state.Behind = behind
			}
		}
		if state.Unpushed > 0 || state.Ahead > 0 || state.Behind > 0 {
			states = append(states, state)
		}
	}

	slices.SortFunc(states, func(a, b BranchStatus) int {
		if d := branchPriority(a) - branchPriority(b); d != 0 {
			return d
		}
		return strings.Compare(a.Name, b.Name)
	})
	return states, anyAhead, anyBehind, nil
}

func branchDivergence(git gitRunner, repo string, ref branchRef) (ahead, behind int, err error) {
	out, err := git.out(repo, "divergence", "rev-list", "--left-right", "--count", "refs/heads/"+ref.Name+"..."+ref.Upstream)
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("malformed git rev-list --left-right --count output %q", strings.TrimSpace(out))
	}
	ahead, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, err
	}
	behind, err = strconv.Atoi(fields[1])
	return ahead, behind, err
}

func branchPriority(branch BranchStatus) int {
	switch {
	case branch.Upstream == "" && branch.Unpushed > 0:
		return 0
	case branch.UpstreamGone && branch.Unpushed > 0:
		return 1
	case branch.Unpushed > 0:
		return 2
	case branch.Ahead > 0 && branch.Behind > 0:
		return 3
	case branch.Ahead > 0:
		return 4
	default:
		return 5
	}
}
