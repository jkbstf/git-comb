package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/jkbstf/git-comb/internal/comb"
)

// isolateConfig keeps run() tests away from the developer's real git
// configuration (comb.* settings would change behavior) and skips
// when git is absent, since loading settings needs it.
func isolateConfig(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return cfg
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
}

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
	for _, want := range []string{"Usage: git comb", "--short", "--only-dirty", "--exclude-dirty", "--fetch", "--diagnostics", "Exit status"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help output missing %q", want)
		}
	}
}

func TestRunDiagnosticsDoNotOverwrite(t *testing.T) {
	isolateConfig(t)
	diagnostic := filepath.Join(t.TempDir(), "diagnostics.jsonl")
	if err := os.WriteFile(diagnostic, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--diagnostics", diagnostic, t.TempDir()}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	content, err := os.ReadFile(diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "keep\n" {
		t.Fatalf("existing diagnostics changed to %q", content)
	}
}

func TestRunDiagnosticsUsePrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}
	isolateConfig(t)
	diagnostic := filepath.Join(t.TempDir(), "diagnostics.jsonl")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--diagnostics", diagnostic, t.TempDir()}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, want 0: %s", code, stderr.String())
	}
	info, err := os.Stat(diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("diagnostic permissions = %o, want 600", got)
	}
}

func TestRunRejectsRetiredVerboseFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--verbose"}, &stdout, &stderr); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -verbose") {
		t.Errorf("stderr = %q, want unknown verbose flag", stderr.String())
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

func TestRunRejectsBadOnly(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--only", "DX"}, &stdout, &stderr); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown sign") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunOnlyFlagAccepted(t *testing.T) {
	isolateConfig(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--only", "dus", t.TempDir()}, &stdout, &stderr); code != 0 {
		t.Errorf("exit = %d, want 0: %s", code, stderr.String())
	}
	if code := run([]string{"-oDUS", t.TempDir()}, &stdout, &stderr); code != 0 {
		t.Errorf("exit = %d for -oDUS, want 0: %s", code, stderr.String())
	}
}

func TestRunNamedOnlyFlagsCompose(t *testing.T) {
	isolateConfig(t)
	var stdout, stderr bytes.Buffer
	args := []string{"--only-dirty", "--only-unpushed", "--only-ahead", t.TempDir()}
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Errorf("exit = %d, want 0: %s", code, stderr.String())
	}
	want := "checking only uncommitted changes, unpushed commits, and branches ahead of upstream"
	if !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want descriptive combined scope", stderr.String())
	}
}

func TestRunNamedExcludeFlagsCompose(t *testing.T) {
	isolateConfig(t)
	base := t.TempDir()
	gitInit(t, filepath.Join(base, "repo")) // a fresh repo reads EL
	var stdout, stderr bytes.Buffer
	args := []string{"--only-empty", "--only-local", "--exclude-local", base}
	if code := run(args, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d, want 1: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Empty repositories:") || strings.Contains(stdout.String(), "No remotes:") {
		t.Errorf("named exclusion not applied: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "checking only empty repositories") {
		t.Errorf("stderr = %q, want effective selection disclosed", stderr.String())
	}
}

func TestRunExceptFlag(t *testing.T) {
	isolateConfig(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-xAB", t.TempDir()}, &stdout, &stderr); code != 0 {
		t.Errorf("exit = %d, want 0: %s", code, stderr.String())
	}

	// only and except compose: only chooses, except subtracts, and
	// the summary discloses the effective selection.
	stderr.Reset()
	if code := run([]string{"--only", "DUS", "--except", "S", t.TempDir()}, &stdout, &stderr); code != 0 {
		t.Errorf("exit = %d for composed selection, want 0: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "checking only uncommitted changes and unpushed commits") {
		t.Errorf("stderr = %q, want the effective selection disclosed", stderr.String())
	}

	// Excluding every sign is vacuous, not an error: the scan looks
	// for nothing and finds it.
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--except", "DUABSELO", t.TempDir()}, &stdout, &stderr); code != 0 {
		t.Errorf("exit = %d for excluding everything, want 0: %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "0 need attention") {
		t.Errorf("stderr = %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "without checking any states") {
		t.Errorf("stderr = %q, want the empty selection disclosed", stderr.String())
	}
}

// TestRunSignFiltersFromConfig: comb.only and comb.except behave like
// standing flags, and a flag overrides its matching key while still
// composing with the other.
func TestRunSignFiltersFromConfig(t *testing.T) {
	cfg := isolateConfig(t)
	base := t.TempDir()
	gitInit(t, filepath.Join(base, "repo")) // a fresh repo reads EL

	if err := os.WriteFile(cfg, []byte("[comb]\n\texcept = L\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{base}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d, want 1: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Empty repositories:") || strings.Contains(stdout.String(), "No remotes:") {
		t.Errorf("comb.except not applied to the row: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "checking only uncommitted changes, unpushed commits, branches ahead of upstream, branches behind upstream, stashes, empty repositories, and unreachable remotes") {
		t.Errorf("stderr = %q, want config-driven selection disclosed", stderr.String())
	}

	// A flag --only composes with the standing comb.except.
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--only", "EL", base}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d, want 1: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "checking only empty repositories") {
		t.Errorf("stderr = %q, want EL minus L", stderr.String())
	}
}

func TestRunNamedOnlyFiltersFromConfig(t *testing.T) {
	cfg := isolateConfig(t)
	base := t.TempDir()
	gitInit(t, filepath.Join(base, "repo")) // a fresh repo reads EL

	content := "[comb]\n\tonlyEmpty = true\n\tonlyLocal = true\n"
	if err := os.WriteFile(cfg, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{base}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d, want 1: %s", code, stderr.String())
	}
	for _, want := range []string{"Empty repositories:", "No remotes:"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q: %q", want, stdout.String())
		}
	}
	if !strings.Contains(stderr.String(), "checking only empty repositories and repositories without remotes") {
		t.Errorf("stderr = %q, want named config scope", stderr.String())
	}

	// Any command-line only filter replaces the configured only
	// selection as a unit, so a one-off scan is easy to express.
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--only-dirty", base}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, want 0: %s", code, stderr.String())
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "checking only uncommitted changes") {
		t.Errorf("command-line only filter did not replace config: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunNamedExcludeFiltersFromConfig(t *testing.T) {
	cfg := isolateConfig(t)
	base := t.TempDir()
	gitInit(t, filepath.Join(base, "repo")) // a fresh repo reads EL

	content := "[comb]\n\texcludeEmpty = true\n\texcludeLocal = true\n"
	if err := os.WriteFile(cfg, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{base}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, want 0: %s", code, stderr.String())
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "checking only uncommitted changes, unpushed commits, branches ahead of upstream, branches behind upstream, stashes, and unreachable remotes") {
		t.Errorf("configured exclusions not applied: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	// Any command-line exclude filter replaces the configured exclude
	// selection as a unit, matching the named only-filter precedence.
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--exclude-dirty", base}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d, want 1: %s", code, stderr.String())
	}
	for _, want := range []string{"Empty repositories:", "No remotes:"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("command-line exclusion did not replace config; missing %q: %q", want, stdout.String())
		}
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
	isolateConfig(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{filepath.Join(t.TempDir(), "missing")}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

func TestRunFileRootExitsTwo(t *testing.T) {
	isolateConfig(t)
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
	isolateConfig(t)
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
	if strings.HasPrefix(stderr.String(), "\n") {
		t.Errorf("empty output received an unnecessary leading separator: %q", stderr.String())
	}
}

func TestRunAcceptsTrailingFlags(t *testing.T) {
	isolateConfig(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{t.TempDir(), "-a"}, &stdout, &stderr); code != 0 {
		t.Errorf("exit = %d, want 0 (trailing flags must parse): %s", code, stderr.String())
	}
}

func TestRunDashDashStopsFlagParsing(t *testing.T) {
	isolateConfig(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--", t.TempDir()}, &stdout, &stderr); code != 0 {
		t.Errorf("exit = %d, want 0: %s", code, stderr.String())
	}
}

// TestRunReadsPruneFromConfig: comb.prune in git config behaves like
// a standing --prune flag.
func TestRunReadsPruneFromConfig(t *testing.T) {
	cfg := isolateConfig(t)
	base := t.TempDir()
	gitInit(t, filepath.Join(base, "skipme", "repo"))

	var stdout, stderr bytes.Buffer
	if code := run([]string{base}, &stdout, &stderr); code == 2 {
		t.Fatalf("baseline run failed: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "combed 1 repository") {
		t.Fatalf("baseline should find the repo: %q", stderr.String())
	}

	if err := os.WriteFile(cfg, []byte("[comb]\n\tprune = skipme\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{base}, &stdout, &stderr); code != 0 {
		t.Errorf("exit = %d, want 0: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "combed 0 repositories") {
		t.Errorf("comb.prune not applied: %q", stderr.String())
	}
}

func TestExpandShortFlags(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"combined booleans", []string{"-sa"}, []string{"-s", "-a"}},
		{"retired verbose short left for the parser", []string{"-sva"}, []string{"-sva"}},
		{"retired fetch short left for the parser", []string{"-fsa"}, []string{"-fsa"}},
		{"attached jobs value", []string{"-j4"}, []string{"-j", "4"}},
		{"attached multi-digit", []string{"-j16"}, []string{"-j", "16"}},
		{"attached only value", []string{"-oDUS"}, []string{"-o", "DUS"}},
		{"attached except value", []string{"-xAB"}, []string{"-x", "AB"}},
		{"attached lowercase only", []string{"-odus"}, []string{"-o", "dus"}},
		{"retired plain short left for the parser", []string{"-v"}, []string{"-v"}},
		{"bare value short untouched", []string{"-o"}, []string{"-o"}},
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
		repos, attention, failed  int
		ackedRepos, ackedBranches int
		signs                     string
		want                      string
	}{
		{0, 0, 0, 0, 0, "", "combed 0 repositories: 0 need attention"},
		{1, 1, 0, 0, 0, "", "combed 1 repository: 1 needs attention"},
		{3, 2, 0, 0, 0, "", "combed 3 repositories: 2 need attention"},
		{3, 1, 2, 0, 0, "", "combed 3 repositories: 1 needs attention, 2 failed"},
		{5, 1, 0, 1, 13, "", "combed 5 repositories: 1 needs attention (1 repository and 13 branches acknowledged)"},
		{5, 0, 0, 2, 0, "", "combed 5 repositories: 0 need attention (2 repositories acknowledged)"},
		{5, 0, 0, 0, 1, "", "combed 5 repositories: 0 need attention (1 branch acknowledged)"},
		{5, 2, 0, 0, 0, "uncommitted changes and unpushed commits", "combed 5 repositories, checking only uncommitted changes and unpushed commits: 2 need attention"},
		{5, 0, 0, 1, 0, "none", "combed 5 repositories without checking any states: 0 need attention (1 repository acknowledged)"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := summary(tt.repos, tt.attention, tt.failed, tt.ackedRepos, tt.ackedBranches, tt.signs)
			if got != tt.want {
				t.Errorf("summary = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSelectedStatesUsesWordsInsteadOfSigns(t *testing.T) {
	only, err := comb.ParseSignSet("DUA")
	if err != nil {
		t.Fatal(err)
	}
	want := "uncommitted changes, unpushed commits, and branches ahead of upstream"
	if got := selectedStates(only); got != want {
		t.Errorf("selectedStates = %q, want %q", got, want)
	}
}

func TestNamedFilterFlagsCoverEveryState(t *testing.T) {
	flags := namedFilterFlags{
		Dirty: true, Unpushed: true, Ahead: true, Behind: true,
		Stashed: true, Empty: true, Local: true, Offline: true,
	}
	if got, want := flags.signs(), "DUABSELO"; got != want {
		t.Errorf("signs = %q, want %q", got, want)
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

func TestOutputWidthUsesGitFallbackForNonTerminal(t *testing.T) {
	t.Setenv("COLUMNS", "160")
	var buf bytes.Buffer
	if got, want := outputWidth(&buf), 80; got != want {
		t.Errorf("outputWidth = %d, want %d", got, want)
	}
}
