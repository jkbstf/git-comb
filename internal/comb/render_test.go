package comb

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func renderFixtures() []Report {
	return []Report{
		{Path: "a/clean", Branch: "master"},
		{Path: "b/dirty", Branch: "main", Dirty: true,
			DirtyStat: ShortStat{FilesChanged: 2, Insertions: 3, Deletions: 1}},
		{Path: "c/unpushed", Branch: "master", Unpushed: 2,
			Branches: []BranchStatus{{Name: "backup/x", Unpushed: 2}}},
		{Path: "d/broken", Err: errors.New("git status: boom")},
		{Path: "e/both", Branch: "topic", Dirty: true, Unpushed: 1,
			DirtyStat: ShortStat{Untracked: 1},
			Branches:  []BranchStatus{{Name: "topic", Unpushed: 1, InWorktree: true}}},
		{Path: "z/acked", Branch: "master", Ignored: true},
	}
}

func TestRenderDetailed(t *testing.T) {
	reports := renderFixtures()

	t.Run("keeps each repository together without signs and counts it once", func(t *testing.T) {
		var buf bytes.Buffer
		attention, failed := Render(&buf, reports, RenderOptions{})
		if attention != 3 || failed != 1 {
			t.Errorf("attention, failed = %d, %d; want 3, 1", attention, failed)
		}
		want := "b/dirty  [main]\n" +
			"  working tree  2 files changed: +3/-1\n" +
			"\nc/unpushed  [master]\n" +
			"  branch    backup/x  2 unpushed commits, no upstream\n" +
			"\nd/broken\n" +
			"  inspection  git status: boom\n" +
			"\ne/both  [topic]\n" +
			"  working tree     1 untracked file\n" +
			"  branch  * topic  1 unpushed commit, no upstream\n"
		if got := buf.String(); got != want {
			t.Errorf("output:\n%q\nwant:\n%q", got, want)
		}
		if strings.Contains(buf.String(), "z/acked") || strings.Contains(buf.String(), "a/clean") {
			t.Errorf("clean or acknowledged repository rendered:\n%s", buf.String())
		}
	})

	t.Run("all adds clean blocks and detailed output includes branches", func(t *testing.T) {
		var buf bytes.Buffer
		Render(&buf, reports, RenderOptions{All: true})
		out := buf.String()
		for _, want := range []string{
			"  branch    backup/x  2 unpushed commits, no upstream\n",
			"  branch  * topic  1 unpushed commit, no upstream\n",
			"a/clean  [master]\n  status  clean\n",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "z/acked") {
			t.Errorf("acknowledged repository rendered even with All:\n%s", out)
		}
	})

	t.Run("color highlights paths and branches", func(t *testing.T) {
		var buf bytes.Buffer
		Render(&buf, reports[1:2], RenderOptions{Color: true})
		out := buf.String()
		if !strings.Contains(out, ansiRed) || !strings.Contains(out, ansiGreen) {
			t.Errorf("color output missing ANSI sequences:\n%q", out)
		}
	})

	t.Run("color highlights only the checked-out branch needing attention", func(t *testing.T) {
		only, err := ParseSignSet("U")
		if err != nil {
			t.Fatal(err)
		}
		report := Report{
			Path: "repo", Branch: "main", Unpushed: 3,
			Branches: []BranchStatus{
				{Name: "main", Unpushed: 1, InWorktree: true},
				{Name: "topic", Unpushed: 2, InWorktree: true},
			},
		}
		var buf bytes.Buffer
		Render(&buf, []Report{report}, RenderOptions{Color: true, Only: only})
		out := buf.String()
		if !strings.Contains(out, "branch  * "+ansiGreen+"main"+ansiReset+"   1 unpushed commit, no upstream") {
			t.Errorf("checked-out branch is not green:\n%q", out)
		}
		if strings.Contains(out, ansiGreen+"topic") {
			t.Errorf("non-checked-out branch is green:\n%q", out)
		}
		if !strings.Contains(out, "branch  + topic  2 unpushed commits, no upstream") {
			t.Errorf("branch checked out in another worktree lacks Git's + marker:\n%q", out)
		}
	})

	t.Run("only selects groups and attention", func(t *testing.T) {
		only, err := ParseSignSet("D")
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		attention, failed := Render(&buf, reports, RenderOptions{Only: only})
		if attention != 2 || failed != 1 {
			t.Errorf("attention, failed = %d, %d; want 2, 1", attention, failed)
		}
		out := buf.String()
		if strings.Contains(out, "branch") || strings.Contains(out, "c/unpushed") {
			t.Errorf("unselected group survived:\n%s", out)
		}
		if !strings.Contains(out, "working tree") || !strings.Contains(out, "d/broken") {
			t.Errorf("selected group or probe failure missing:\n%s", out)
		}
	})

	t.Run("branch filters do not leak unselected status", func(t *testing.T) {
		only, err := ParseSignSet("A")
		if err != nil {
			t.Fatal(err)
		}
		report := Report{
			Path: "repo", Branch: "main", Unpushed: 2, Ahead: true, Behind: true,
			Branches: []BranchStatus{{
				Name: "main", Upstream: "origin/main", Unpushed: 2, Ahead: 5, Behind: 3, InWorktree: true,
			}},
		}
		var buf bytes.Buffer
		Render(&buf, []Report{report}, RenderOptions{Only: only})
		out := buf.String()
		if !strings.Contains(out, "main  [origin/main]  5 commits ahead") {
			t.Errorf("selected ahead status missing:\n%s", out)
		}
		if strings.Contains(out, "unpushed commit") || strings.Contains(out, "behind") {
			t.Errorf("unselected branch status leaked:\n%s", out)
		}
	})
}

func TestRenderUsesPathsRelativeToNearestScanRoot(t *testing.T) {
	root := t.TempDir()
	group := filepath.Join(root, "group")
	repo := filepath.Join(group, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	reports := []Report{{Path: repo, Branch: "main", Dirty: true}}

	var detailed bytes.Buffer
	Render(&detailed, reports, RenderOptions{Roots: []string{root, group}})
	if got, want := detailed.String(), "repo  [main]\n  working tree\n"; got != want {
		t.Errorf("detailed output = %q, want %q", got, want)
	}

	var short bytes.Buffer
	Render(&short, reports, RenderOptions{Roots: []string{root}, Short: true})
	wantShort := "D      " + filepath.Join("group", "repo") + "\n"
	if got := short.String(); got != wantShort {
		t.Errorf("short output = %q, want %q", got, wantShort)
	}
}

func TestRenderShortPreservesCompactView(t *testing.T) {
	reports := renderFixtures()
	var buf bytes.Buffer
	attention, failed := Render(&buf, reports, RenderOptions{Short: true})
	if attention != 3 || failed != 1 {
		t.Errorf("attention, failed = %d, %d; want 3, 1", attention, failed)
	}
	want := "D      b/dirty\n" +
		"U      c/unpushed\n" +
		"!      d/broken: git status: boom\n" +
		"DU     e/both\n"
	if got := buf.String(); got != want {
		t.Errorf("output:\n%q\nwant:\n%q", got, want)
	}
}

func TestRenderDetailedCoversEveryNamedState(t *testing.T) {
	reports := []Report{{
		Path:        "repo",
		Branch:      "main",
		Dirty:       true,
		Unpushed:    2,
		Ahead:       true,
		Behind:      true,
		Stashes:     3,
		Empty:       true,
		NoRemote:    true,
		FetchFailed: true,
		Branches: []BranchStatus{{
			Name: "main", Upstream: "origin/main", Unpushed: 2, Ahead: 2, Behind: 1, InWorktree: true,
		}},
	}}
	var buf bytes.Buffer
	attention, failed := Render(&buf, reports, RenderOptions{})
	if attention != 1 || failed != 0 {
		t.Errorf("attention, failed = %d, %d; want 1, 0", attention, failed)
	}
	for _, row := range []string{
		"working tree",
		"remotes",
		"branch",
		"stash",
		"repository",
		"fetch",
	} {
		if !strings.Contains(buf.String(), row) {
			t.Errorf("output missing %q:\n%s", row, buf.String())
		}
	}
	for _, want := range []string{
		"repo  [main]\n",
		"working tree",
		"remotes                        none configured",
		"stash                          3 stashes",
		"repository                     empty",
		"fetch                          one or more remotes unreachable",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("output missing repo-level context %q:\n%s", want, buf.String())
		}
	}
	out := buf.String()
	dirtyAt := strings.Index(out, "working tree")
	localAt := strings.Index(out, "remotes")
	branchesAt := strings.Index(out, "branch")
	stashesAt := strings.Index(out, "stash")
	if dirtyAt >= localAt || localAt >= branchesAt || branchesAt >= stashesAt {
		t.Errorf("important sections are out of order:\n%s", out)
	}
	if strings.Contains(buf.String(), "DUABSELO") {
		t.Errorf("detailed output exposed compact signs:\n%s", buf.String())
	}
}

func TestBranchStatusParts(t *testing.T) {
	tests := []struct {
		name                    string
		branch                  BranchStatus
		wantContext, wantDetail string
	}{
		{"no upstream", BranchStatus{Name: "local", Unpushed: 2}, "", "2 unpushed commits, no upstream"},
		{"ahead all unpushed", BranchStatus{Name: "main", Upstream: "origin/main", Ahead: 2, Unpushed: 2}, "[origin/main]", "2 unpushed commits"},
		{"ahead partly unpushed", BranchStatus{Name: "main", Upstream: "origin/main", Ahead: 5, Unpushed: 2}, "[origin/main]", "2 unpushed commits"},
		{"diverged and unpushed", BranchStatus{Name: "main", Upstream: "origin/main", Ahead: 5, Behind: 3, Unpushed: 2}, "[origin/main]", "2 unpushed commits, 3 commits behind"},
		{"ahead but present on a remote", BranchStatus{Name: "main", Upstream: "origin/main", Ahead: 2}, "[origin/main]", "2 commits ahead"},
		{"behind", BranchStatus{Name: "main", Upstream: "origin/main", Behind: 1}, "[origin/main]", "1 commit behind"},
		{"upstream gone", BranchStatus{Name: "topic", Upstream: "origin/topic", UpstreamGone: true, Unpushed: 1}, "[origin/topic]", "1 unpushed commit, upstream gone"},
		{"detached", BranchStatus{Name: "(detached HEAD)", Unpushed: 1, Detached: true}, "", "1 unpushed commit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := branchTableRow(tt.branch, "", renderColors{})
			if got, _ := fitLabel(row.context, 100); got != tt.wantContext {
				t.Errorf("context = %q, want %q", got, tt.wantContext)
			}
			if got := row.detail; got != tt.wantDetail {
				t.Errorf("detail = %q, want %q", got, tt.wantDetail)
			}
		})
	}
}

func TestGroupedColumnsAlignAndTrimNamesIndependently(t *testing.T) {
	reports := []Report{{
		Path: "repo", Branch: "main", Unpushed: 2,
		Branches: []BranchStatus{
			{Name: "short", Unpushed: 1},
			{
				Name: "feature/very-long-description/recognizable-tail", Upstream: "origin/very-long-upstream/recognizable-end",
				Unpushed: 1,
			},
			{Name: "tracked-short", Upstream: "origin/main", Unpushed: 1},
		},
	}}
	var buf bytes.Buffer
	Render(&buf, reports, RenderOptions{Width: 60})

	var detailLines []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "unpushed commit") {
			detailLines = append(detailLines, line)
		}
	}
	if len(detailLines) != 3 {
		t.Fatalf("detail lines = %q, want 3", detailLines)
	}
	firstContext := strings.Index(detailLines[1], "[")
	if secondContext := strings.Index(detailLines[2], "["); secondContext != firstContext {
		t.Fatalf("context columns do not align:\n%s", buf.String())
	}
	firstDetail := strings.Index(detailLines[0], "1 unpushed commit")
	for _, line := range detailLines[1:] {
		if detail := strings.Index(line, "1 unpushed commit"); detail != firstDetail {
			t.Fatalf("detail columns do not align:\n%s", buf.String())
		}
	}
	if strings.Count(detailLines[1], "...") < 2 ||
		!strings.Contains(detailLines[1], "featu...tail") ||
		!strings.Contains(detailLines[1], "origi...-end") {
		t.Fatalf("branch and upstream did not preserve both ends:\n%s", buf.String())
	}
}

func TestMiddleTrimPreservesBothEnds(t *testing.T) {
	if got, want := middleTrim("abcdefghijkl", 9), "abc...jkl"; got != want {
		t.Errorf("middleTrim = %q, want %q", got, want)
	}
	if got, want := middleTrim("zażółćgęślą", 9), "zaż...ślą"; got != want {
		t.Errorf("Unicode middleTrim = %q, want %q", got, want)
	}
}

func TestFormatShortStat(t *testing.T) {
	tests := []struct {
		name string
		stat ShortStat
		want string
	}{
		{"tracked", ShortStat{FilesChanged: 2, Insertions: 3, Deletions: 1}, "2 files changed: +3/-1"},
		{"insertions only", ShortStat{FilesChanged: 1, Insertions: 3}, "1 file changed: +3/-0"},
		{"binary", ShortStat{FilesChanged: 1}, "1 file changed"},
		{"untracked", ShortStat{Untracked: 1}, "1 untracked file"},
		{"mixed singular", ShortStat{FilesChanged: 1, Insertions: 1, Deletions: 1, Untracked: 2}, "1 file changed: +1/-1, 2 untracked files"},
		{"empty", ShortStat{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatShortStat(tt.stat); got != tt.want {
				t.Errorf("formatShortStat = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderNoColorMeansNoEscapes(t *testing.T) {
	var buf bytes.Buffer
	Render(&buf, renderFixtures(), RenderOptions{All: true})
	if strings.Contains(buf.String(), "\x1b") {
		t.Errorf("plain output contains ANSI escapes:\n%q", buf.String())
	}
}

func TestRenderedInterfaceUsesASCIIForASCIIInput(t *testing.T) {
	reports := []Report{{
		Path: "repo", Branch: "main", Dirty: true, Unpushed: 2, Ahead: true, Behind: true,
		DirtyStat: ShortStat{FilesChanged: 1, Insertions: 2, Deletions: 1, Untracked: 1},
		Branches: []BranchStatus{
			{Name: "main", Upstream: "origin/main", Unpushed: 1, Ahead: 2, InWorktree: true},
			{Name: "topic", Upstream: "origin/topic", Unpushed: 1, UpstreamGone: true},
		},
	}}
	var buf bytes.Buffer
	Render(&buf, reports, RenderOptions{})
	for _, r := range buf.String() {
		if r > 127 {
			t.Fatalf("rendered interface contains non-ASCII character %q:\n%s", r, buf.String())
		}
	}
}

func TestDefaultJobsBounds(t *testing.T) {
	n := DefaultJobs()
	if n < 1 || n > 8 {
		t.Errorf("DefaultJobs() = %d, want within [1, 8]", n)
	}
}

func TestPruneListFlagValue(t *testing.T) {
	var p PruneList
	for _, v := range []string{"build", "_deps"} {
		if err := p.Set(v); err != nil {
			t.Fatalf("Set(%q): %v", v, err)
		}
	}
	if got, want := p.String(), "build,_deps"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
