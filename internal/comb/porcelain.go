package comb

import (
	"strconv"
	"strings"
)

// worktreeStatus is the parsed result of
// git status --porcelain=v2 --branch -uall.
type worktreeStatus struct {
	Branch      string
	OID         string
	Detached    bool
	Empty       bool
	HasUpstream bool
	Ahead       int
	Behind      int
	Dirty       bool
}

// parseStatus reads porcelain v2 output, documented as stable in
// git-status(1). Header lines start with "#"; entry lines with "1"
// (changed), "2" (renamed), "u" (unmerged), or "?" (untracked), all
// of which mean dirty. Ignored entries ("!") do not.
func parseStatus(out string) worktreeStatus {
	var st worktreeStatus
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line == "":
		case strings.HasPrefix(line, "# branch.oid "):
			v := strings.TrimPrefix(line, "# branch.oid ")
			if v == "(initial)" {
				st.Empty = true
			} else {
				st.OID = v
			}
		case strings.HasPrefix(line, "# branch.head "):
			v := strings.TrimPrefix(line, "# branch.head ")
			if v == "(detached)" {
				st.Detached = true
			} else {
				st.Branch = v
			}
		case strings.HasPrefix(line, "# branch.ab "):
			// The line is present exactly when an upstream is set,
			// even at +0 -0.
			st.HasUpstream = true
			st.Ahead, st.Behind = parseAheadBehind(strings.TrimPrefix(line, "# branch.ab "))
		case strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "!"):
		default:
			st.Dirty = true
		}
	}
	return st
}

// parseAheadBehind reads the "+<ahead> -<behind>" payload.
func parseAheadBehind(v string) (ahead, behind int) {
	for _, f := range strings.Fields(v) {
		switch {
		case strings.HasPrefix(f, "+"):
			ahead = atoiOrZero(f[1:])
		case strings.HasPrefix(f, "-"):
			behind = atoiOrZero(f[1:])
		}
	}
	return ahead, behind
}

func atoiOrZero(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
