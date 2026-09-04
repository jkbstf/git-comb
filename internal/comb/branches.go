package comb

import (
	"bufio"
	"fmt"
	"math/bits"
	"slices"
	"strconv"
	"strings"
)

// branchRef is the inexpensive for-each-ref view used for every local branch.
type branchRef struct {
	Name, OID, Upstream, UpstreamShort string
	WorktreePath                       string
	Ahead, Behind                      int
	UpstreamGone                       bool
}

func listBranchRefs(git gitRunner, repo string, includeTrack, includeStash bool) ([]branchRef, bool, error) {
	track := ""
	if includeTrack {
		track = "%(upstream:track,nobracket)"
	}
	format := "%(refname)%09%(refname:short)%09%(objectname)%09%(upstream)%09%(upstream:short)%09" + track + "%09%(worktreepath)"
	args := []string{"for-each-ref", "refs/heads"}
	if includeStash {
		args = append(args, "refs/stash")
	}
	args = append(args, "--format="+format)
	out, err := git.out(repo, "branches", args...)
	if err != nil {
		return nil, false, err
	}

	var refs []branchRef
	hasStash := false
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 7)
		if len(fields) != 7 {
			return nil, false, fmt.Errorf("malformed git for-each-ref line %q", line)
		}
		if fields[0] == "refs/stash" {
			hasStash = true
			continue
		}
		ahead, behind, gone, err := parseTracking(fields[5])
		if err != nil {
			return nil, false, err
		}
		refs = append(refs, branchRef{
			Name: fields[1], OID: fields[2], Upstream: fields[3], UpstreamShort: fields[4],
			Ahead: ahead, Behind: behind, UpstreamGone: gone, WorktreePath: fields[6],
		})
	}
	return refs, hasStash, nil
}

func parseTracking(description string) (ahead, behind int, gone bool, err error) {
	if description == "gone" {
		return 0, 0, true, nil
	}
	if description == "" {
		return 0, 0, false, nil
	}
	fields := strings.Fields(strings.ReplaceAll(description, ",", ""))
	if len(fields) == 0 || len(fields)%2 != 0 {
		return 0, 0, false, fmt.Errorf("malformed git tracking description %q", description)
	}
	for i := 0; i < len(fields); i += 2 {
		n, parseErr := strconv.Atoi(fields[i+1])
		if parseErr != nil {
			return 0, 0, false, fmt.Errorf("malformed git tracking description %q", description)
		}
		switch fields[i] {
		case "ahead":
			ahead = n
		case "behind":
			behind = n
		default:
			return 0, 0, false, fmt.Errorf("malformed git tracking description %q", description)
		}
	}
	return ahead, behind, false, nil
}

func branchNames(refs []branchRef) []string {
	names := make([]string, len(refs))
	for i, ref := range refs {
		names[i] = ref.Name
	}
	return names
}

// localOnlyBranchCounts asks Git for the local-only commit graph once, then
// computes both its union size and the exact count reachable from each branch.
// Topological order guarantees that every child contributes its branch mask to
// a parent before that parent is emitted. This preserves overlapping-history
// semantics without one process per branch.
func localOnlyBranchCounts(git gitRunner, repo string, refs []branchRef, kept []string) (int, map[string]int, error) {
	keptSet := make(map[string]bool, len(kept))
	for _, name := range kept {
		keptSet[name] = true
	}
	selected := make([]branchRef, 0, len(kept))
	args := []string{"rev-list", "--topo-order", "--parents"}
	for _, ref := range refs {
		if keptSet[ref.Name] {
			selected = append(selected, ref)
			args = append(args, ref.OID)
		}
	}
	if len(selected) != len(kept) {
		return 0, nil, fmt.Errorf("local branch changed during inspection")
	}
	args = append(args, "--not", "--remotes")
	out, err := git.out(repo, "unpushed_graph", args...)
	if err != nil {
		return 0, nil, err
	}
	return parseLocalOnlyGraph(out, selected)
}

func parseLocalOnlyGraph(out string, refs []branchRef) (int, map[string]int, error) {
	words := (len(refs) + 63) / 64
	reachable := make(map[string][]uint64)
	branchCounts := make([]int, len(refs))
	for i, ref := range refs {
		mask := reachable[ref.OID]
		if mask == nil {
			mask = make([]uint64, words)
			reachable[ref.OID] = mask
		}
		mask[i/64] |= uint64(1) << uint(i%64)
	}

	total := 0
	scanner := bufio.NewScanner(strings.NewReader(out))
	// An octopus merge can have an unusually long parent line. The normal
	// scanner limit is unnecessarily restrictive for otherwise valid Git data.
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) == 0 {
			return 0, nil, fmt.Errorf("malformed local-only commit graph")
		}
		mask := reachable[fields[0]]
		if mask == nil {
			return 0, nil, fmt.Errorf("malformed local-only commit graph")
		}
		total++
		for wordIndex, word := range mask {
			for word != 0 {
				branch := wordIndex*64 + bits.TrailingZeros64(word)
				if branch < len(branchCounts) {
					branchCounts[branch]++
				}
				word &= word - 1
			}
		}
		movedMask := false
		for _, parent := range fields[1:] {
			parentMask := reachable[parent]
			if parentMask == nil {
				if !movedMask {
					// A linear history can hand the mask directly to its
					// parent. Allocate only for additional merge parents.
					parentMask = mask
					movedMask = true
				} else {
					parentMask = make([]uint64, words)
					copy(parentMask, mask)
				}
				reachable[parent] = parentMask
			} else {
				for i := range mask {
					parentMask[i] |= mask[i]
				}
			}
		}
		delete(reachable, fields[0])
	}
	if err := scanner.Err(); err != nil {
		return 0, nil, fmt.Errorf("read local-only commit graph: %w", err)
	}
	counts := make(map[string]int, len(refs))
	for i, ref := range refs {
		counts[ref.Name] = branchCounts[i]
	}
	return total, counts, nil
}

// inspectBranches combines global local-only work with each branch's
// upstream relationship. Per-branch counts are populated by the detailed
// view's single local-only graph walk; the short view needs only the aggregate.
func inspectBranches(refs []branchRef, kept []string, unpushed map[string]int, opts Options) (states []BranchStatus, anyAhead, anyBehind bool) {
	needUnpushed := opts.Only.Has('U')
	needAhead := opts.Only.Has('A')
	needBehind := opts.Only.Has('B')
	keptSet := make(map[string]bool, len(kept))
	for _, name := range kept {
		keptSet[name] = true
	}

	for _, ref := range refs {
		trackAhead := ref.Ahead > 0
		trackBehind := ref.Behind > 0
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
			UpstreamGone: ref.UpstreamGone,
		}
		if needUnpushed && keptSet[ref.Name] {
			state.Unpushed = unpushed[ref.Name]
		}
		if needAhead {
			state.Ahead = ref.Ahead
		}
		if needBehind {
			state.Behind = ref.Behind
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
	return states, anyAhead, anyBehind
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
