package comb

import (
	"strings"
)

// worktreeStatus is the parsed result of
// git status --porcelain=v2 --branch -uall.
type worktreeStatus struct {
	Branch   string
	OID      string
	Detached bool
	// Unborn means HEAD points at a branch with no commit yet — a
	// fresh repository, or an orphan checkout in an old one.
	Unborn bool
	Dirty  bool
	// Untracked counts individual untracked files. Status is invoked
	// with -uall, so untracked directories are expanded into files.
	Untracked int
}

// parseStatus reads porcelain v2 output, documented as stable in
// git-status(1). Header lines start with "#"; entry lines with "1"
// (changed), "2" (renamed), "u" (unmerged), or "?" (untracked), all
// of which mean dirty. Ignored entries ("!") do not. The branch.ab
// header is present exactly when an upstream is configured.
func parseStatus(out string) worktreeStatus {
	var st worktreeStatus
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line == "":
		case strings.HasPrefix(line, "# branch.oid "):
			v := strings.TrimPrefix(line, "# branch.oid ")
			if v == "(initial)" {
				st.Unborn = true
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
		case strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "!"):
		case strings.HasPrefix(line, "? "):
			st.Dirty = true
			st.Untracked++
		default:
			st.Dirty = true
		}
	}
	return st
}
