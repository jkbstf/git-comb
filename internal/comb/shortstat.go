package comb

import (
	"fmt"
	"strconv"
	"strings"
)

// worktreeShortStat summarizes the complete tracked working tree,
// staged and unstaged together. On an unborn branch, an empty tree is
// used in place of HEAD. Untracked files come from porcelain status
// because a Git diff deliberately excludes them.
func worktreeShortStat(repo string, st worktreeStatus) (ShortStat, error) {
	base := "HEAD"
	if st.Unborn {
		emptyTree, err := gitOutInput(repo, "", "hash-object", "-t", "tree", "--stdin")
		if err != nil {
			return ShortStat{}, err
		}
		base = strings.TrimSpace(emptyTree)
	}

	out, err := gitOut(repo, "diff", "--no-ext-diff", "--numstat", base, "--")
	if err != nil {
		return ShortStat{}, err
	}
	stat, err := parseNumstat(out)
	if err != nil {
		return ShortStat{}, err
	}
	stat.Untracked = st.Untracked
	return stat, nil
}

func parseNumstat(out string) (ShortStat, error) {
	var stat ShortStat
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			return ShortStat{}, fmt.Errorf("malformed git diff --numstat line %q", line)
		}
		insertions, err := numstatCount(fields[0])
		if err != nil {
			return ShortStat{}, err
		}
		deletions, err := numstatCount(fields[1])
		if err != nil {
			return ShortStat{}, err
		}
		stat.FilesChanged++
		stat.Insertions += insertions
		stat.Deletions += deletions
	}
	return stat, nil
}

func numstatCount(value string) (int, error) {
	if value == "-" { // binary file
		return 0, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("malformed git diff --numstat count %q", value)
	}
	return n, nil
}
