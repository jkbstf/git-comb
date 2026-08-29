package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRunVersionGoesToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, &stdout, &stderr); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.HasPrefix(stdout.String(), "git-comb ") {
		t.Errorf("stdout = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	for _, want := range []string{"Usage: git comb", "--fetch", "Exit status"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help output missing %q", want)
		}
	}
}

func TestRunRejectsUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--nonsense"}, &stdout, &stderr); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "git-comb:") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunRejectsBadColor(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--color", "sometimes"}, &stdout, &stderr); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "invalid --color") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunMissingRootExitsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{filepath.Join(t.TempDir(), "missing")}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

func TestRunFileRootExitsTwo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{path}, &stdout, &stderr); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

func TestRunEmptyTreeExitsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{t.TempDir()}, &stdout, &stderr); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty (summary belongs on stderr)", stdout.String())
	}
	if !strings.Contains(stderr.String(), "combed 0 repositories") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunAcceptsTrailingFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{t.TempDir(), "-a"}, &stdout, &stderr); code != 0 {
		t.Errorf("exit = %d, want 0 (trailing flags must parse): %s", code, stderr.String())
	}
}

func TestRunDashDashStopsFlagParsing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--", t.TempDir()}, &stdout, &stderr); code != 0 {
		t.Errorf("exit = %d, want 0: %s", code, stderr.String())
	}
}

func TestExpandShortFlags(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"combined booleans", []string{"-fv"}, []string{"-f", "-v"}},
		{"triple", []string{"-fva"}, []string{"-f", "-v", "-a"}},
		{"attached jobs value", []string{"-j4"}, []string{"-j", "4"}},
		{"attached multi-digit", []string{"-j16"}, []string{"-j", "16"}},
		{"plain short untouched", []string{"-f"}, []string{"-f"}},
		{"long flags untouched", []string{"--fetch"}, []string{"--fetch"}},
		{"unknown combo left for the parser", []string{"-fx"}, []string{"-fx"}},
		{"positional untouched", []string{"dir"}, []string{"dir"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := expandShortFlags(tt.in); !slices.Equal(got, tt.want) {
				t.Errorf("expandShortFlags(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSummary(t *testing.T) {
	tests := []struct {
		repos, attention, failed int
		want                     string
	}{
		{0, 0, 0, "combed 0 repositories in 5ms: 0 need attention"},
		{1, 1, 0, "combed 1 repository in 5ms: 1 needs attention"},
		{3, 2, 0, "combed 3 repositories in 5ms: 2 need attention"},
		{3, 1, 2, "combed 3 repositories in 5ms: 1 needs attention, 2 failed"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := summary(tt.repos, tt.attention, tt.failed, 5*time.Millisecond)
			if got != tt.want {
				t.Errorf("summary = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFmtDuration(t *testing.T) {
	if got := fmtDuration(300 * time.Microsecond); got != "<1ms" {
		t.Errorf("fmtDuration(<1ms) = %q", got)
	}
	if got := fmtDuration(1490 * time.Millisecond); got != "1.49s" {
		t.Errorf("fmtDuration = %q, want 1.49s", got)
	}
}

func TestColorEnabled(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	if on, err := colorEnabled("always", os.Stdout); err != nil || !on {
		t.Errorf("always = %v, %v", on, err)
	}
	if on, err := colorEnabled("never", os.Stdout); err != nil || on {
		t.Errorf("never = %v, %v", on, err)
	}
	if _, err := colorEnabled("sometimes", os.Stdout); err == nil {
		t.Error("invalid value accepted")
	}
	var buf bytes.Buffer
	if on, _ := colorEnabled("auto", &buf); on {
		t.Error("auto = true for a non-terminal writer")
	}
	t.Setenv("NO_COLOR", "1")
	if on, _ := colorEnabled("auto", os.Stdout); on {
		t.Error("auto ignored NO_COLOR")
	}
}
