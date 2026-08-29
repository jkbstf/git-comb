package comb

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	reports := []Report{
		{Path: "a/clean", Branch: "master"},
		{Path: "b/dirty", Branch: "main", Dirty: true},
		{Path: "c/unpushed", Branch: "master", Unpushed: 2,
			UnpushedBranches: []BranchCount{{Name: "backup/x", Commits: 2}}},
		{Path: "d/broken", Err: errors.New("git status: boom")},
	}

	t.Run("default hides clean, counts attention and failures", func(t *testing.T) {
		var buf bytes.Buffer
		attention, failed := Render(&buf, reports, RenderOptions{})
		if attention != 2 || failed != 1 {
			t.Errorf("attention, failed = %d, %d; want 2, 1", attention, failed)
		}
		out := buf.String()
		if strings.Contains(out, "a/clean") {
			t.Errorf("clean repository rendered without All:\n%s", out)
		}
		for _, want := range []string{
			"D      b/dirty [main]\n",
			"U      c/unpushed [master]\n",
			"!      d/broken: git status: boom\n",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "unpushed:") {
			t.Errorf("branch detail rendered without Verbose:\n%s", out)
		}
	})

	t.Run("all and verbose add clean rows and branch detail", func(t *testing.T) {
		var buf bytes.Buffer
		Render(&buf, reports, RenderOptions{All: true, Verbose: true})
		out := buf.String()
		for _, want := range []string{
			"       a/clean [master]\n",
			"       unpushed: backup/x (2)\n",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("color wraps signs and branch", func(t *testing.T) {
		var buf bytes.Buffer
		Render(&buf, reports[1:2], RenderOptions{Color: true})
		out := buf.String()
		if !strings.Contains(out, ansiRed) || !strings.Contains(out, ansiGreen) {
			t.Errorf("color output missing ANSI sequences:\n%q", out)
		}
	})

	t.Run("no color means no escapes", func(t *testing.T) {
		var buf bytes.Buffer
		Render(&buf, reports, RenderOptions{All: true, Verbose: true})
		if strings.Contains(buf.String(), "\x1b") {
			t.Errorf("plain output contains ANSI escapes:\n%q", buf.String())
		}
	})
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
