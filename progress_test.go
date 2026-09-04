package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/jkbstf/git-comb/internal/comb"
)

func TestTerminalProgressDiscoveryAndCheckingLines(t *testing.T) {
	started := time.Now().Add(-2 * time.Second)
	var buf bytes.Buffer
	p := &terminalProgress{
		w: &buf, ansi: true, started: started, width: 120, phase: "scanning",
		entries: 290700, dirs: 35642, found: 42,
		current: "large/subtree", currentAt: started,
		operations: make(map[string]activeProgressOperation),
	}
	p.drawLocked(time.Now())
	out := buf.String()
	for _, want := range []string{"Scanning", "290,700 entries", "35,642 dirs", "42 found", "in large/subtree"} {
		if !strings.Contains(out, want) {
			t.Errorf("discovery progress missing %q: %q", want, out)
		}
	}

	buf.Reset()
	p.lines = 0
	p.phase, p.total, p.checked, p.active, p.attention = "checking", 42, 31, 8, 7
	p.operations["repo"] = activeProgressOperation{name: "status", started: started}
	p.drawLocked(time.Now())
	out = buf.String()
	for _, want := range []string{"Checking", "31/42", "8 active", "7 need attention", "slowest: repo | status"} {
		if !strings.Contains(out, want) {
			t.Errorf("checking progress missing %q: %q", want, out)
		}
	}
}

func TestTerminalProgressNarrowLineOmitsDetail(t *testing.T) {
	var buf bytes.Buffer
	p := &terminalProgress{
		w: &buf, ansi: true, started: time.Now().Add(-2 * time.Second), width: 50,
		phase: "checking", total: 42, checked: 31, active: 8, attention: 7,
		operations: map[string]activeProgressOperation{
			"repo": {name: "status", started: time.Now().Add(-2 * time.Second)},
		},
	}
	p.drawLocked(time.Now())
	out := buf.String()
	if strings.Contains(out, "slowest:") || strings.Count(out, "\n") != 0 {
		t.Errorf("narrow progress used a detail line: %q", out)
	}
	if !strings.Contains(out, "Checking") {
		t.Errorf("narrow progress missing phase: %q", out)
	}
}

func TestTerminalProgressFallsBackWithoutANSI(t *testing.T) {
	var buf bytes.Buffer
	p := &terminalProgress{
		w: &buf, started: time.Now().Add(-time.Second), width: 80,
		phase: "checking", total: 2, checked: 1, active: 1,
		operations: make(map[string]activeProgressOperation),
	}
	p.drawLocked(time.Now())
	p.clearLocked()
	if strings.Contains(buf.String(), "\x1b") {
		t.Errorf("fallback progress contains ANSI escapes: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "\r") || !strings.Contains(buf.String(), "Checking") {
		t.Errorf("fallback progress missing carriage-return status: %q", buf.String())
	}
}

func TestTerminalProgressTracksConcurrentEvents(t *testing.T) {
	p := &terminalProgress{operations: make(map[string]activeProgressOperation)}
	p.Update(comb.ProgressEvent{Kind: comb.ProgressPhase, Phase: "checking", Total: 2})
	p.Update(comb.ProgressEvent{Kind: comb.ProgressRepositoryStart, Path: "a", Total: 2})
	p.Update(comb.ProgressEvent{Kind: comb.ProgressRepositoryStart, Path: "b", Total: 2})
	p.Update(comb.ProgressEvent{Kind: comb.ProgressRepositoryEnd, Path: "a", Attention: true})
	p.Update(comb.ProgressEvent{Kind: comb.ProgressRepositoryEnd, Path: "b", Failed: true})
	if p.active != 0 || p.checked != 2 || p.attention != 1 || p.failed != 1 {
		t.Errorf("progress totals = active %d checked %d attention %d failed %d", p.active, p.checked, p.attention, p.failed)
	}
}

func TestTerminalProgressAutoDisablesForNonTerminal(t *testing.T) {
	if progress := newTerminalProgress(&bytes.Buffer{}, []string{"."}); progress != nil {
		t.Error("progress enabled for a non-terminal writer")
	}
}
