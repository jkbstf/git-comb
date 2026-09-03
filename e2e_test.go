package main

// End-to-end tests drive the compiled binary as a black box: real
// argv, real process exit codes, real stream separation — and the
// git subcommand dispatch that no in-process test can reach. TestMain
// builds the binary once; -short or a missing git skips the layer.

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// binPath is the compiled binary under test; empty when the e2e layer
// is skipped.
var binPath string

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(testMain(m))
}

func testMain(m *testing.M) int {
	if testing.Short() {
		return m.Run()
	}
	if _, err := exec.LookPath("git"); err != nil {
		return m.Run()
	}
	dir, err := os.MkdirTemp("", "git-comb-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()

	name := "git-comb"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(dir, name)
	if b, err := exec.Command("go", "build", "-o", out, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building e2e binary: %v\n%s", err, b)
		return 1
	}
	binPath = out
	return m.Run()
}

func requireBinary(t *testing.T) {
	t.Helper()
	if binPath == "" {
		t.Skip("e2e binary not built (-short, or git is not on PATH)")
	}
}

// e2eEnv isolates git config for the child process and installs the
// owner identity fixtures need for committing.
func e2eEnv(t *testing.T) {
	t.Helper()
	cfg := isolateConfig(t)
	content := "[user]\n\tname = Test\n\temail = test@example.invalid\n" +
		"[init]\n\tdefaultBranch = master\n" +
		"[commit]\n\tgpgsign = false\n"
	if err := os.WriteFile(cfg, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func e2eGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

func e2eRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	e2eGit(t, dir, "init", "--quiet")
}

func e2eCommit(t *testing.T, dir, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	e2eGit(t, dir, "add", "--all")
	e2eGit(t, dir, "commit", "--quiet", "-m", msg)
}

// e2eSynced builds a repository with one commit pushed to a local
// bare origin — the clean baseline.
func e2eSynced(t *testing.T, dir, bareDir string) {
	t.Helper()
	e2eRepo(t, dir)
	e2eCommit(t, dir, "content\n", "first")
	if err := os.MkdirAll(bareDir, 0o755); err != nil {
		t.Fatal(err)
	}
	e2eGit(t, bareDir, "init", "--quiet", "--bare")
	e2eGit(t, dir, "remote", "add", "origin", bareDir)
	e2eGit(t, dir, "push", "--quiet", "--set-upstream", "origin", "master")
}

// buildKitchen assembles one tree with every ordinary repository
// state: clean, dirty, acknowledged, remoteless, stashed, unpushed.
// Directory names sort alphabetically, so output order is fixed.
func buildKitchen(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bares := t.TempDir()

	e2eSynced(t, filepath.Join(base, "clean"), filepath.Join(bares, "clean.git"))

	dirty := filepath.Join(base, "dirty")
	e2eSynced(t, dirty, filepath.Join(bares, "dirty.git"))
	if err := os.WriteFile(filepath.Join(dirty, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ignored := filepath.Join(base, "ignored")
	e2eSynced(t, ignored, filepath.Join(bares, "ignored.git"))
	if err := os.WriteFile(filepath.Join(ignored, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e2eGit(t, ignored, "config", "comb.ignore", "true")

	norem := filepath.Join(base, "norem")
	e2eRepo(t, norem)
	e2eCommit(t, norem, "content\n", "first")

	stash := filepath.Join(base, "stash")
	e2eSynced(t, stash, filepath.Join(bares, "stash.git"))
	if err := os.WriteFile(filepath.Join(stash, "file.txt"), []byte("modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e2eGit(t, stash, "stash", "--quiet")

	unpushed := filepath.Join(base, "unpushed")
	e2eSynced(t, unpushed, filepath.Join(bares, "unpushed.git"))
	e2eGit(t, unpushed, "checkout", "--quiet", "-b", "keep/x")
	e2eCommit(t, unpushed, "local only\n", "never pushed")
	e2eGit(t, unpushed, "checkout", "--quiet", "master")

	return base
}

type binResult struct {
	stdout string
	stderr string
	code   int
}

func runBinary(t *testing.T, args ...string) binResult {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("running %s: %v", binPath, err)
		}
		code = exit.ExitCode()
	}
	return binResult{stdout.String(), stderr.String(), code}
}

func row(sign, path string) string {
	return fmt.Sprintf("%-6s %s", sign, path)
}

func TestE2EKitchenSink(t *testing.T) {
	requireBinary(t)
	e2eEnv(t)
	base := buildKitchen(t)

	res := runBinary(t, base)
	if res.code != 1 {
		t.Fatalf("exit = %d, want 1; stderr: %s", res.code, res.stderr)
	}
	want := strings.Join([]string{
		"Uncommitted changes:",
		"  dirty  [master]  1 untracked file",
		"",
		"No remotes:",
		"  norem",
		"",
		"Branches:",
		"  unpushed",
		"      keep/x  1 unpushed commit, no upstream",
		"",
		"Stashes:",
		"  stash  1 stash",
	}, "\n") + "\n"
	if res.stdout != want {
		t.Errorf("stdout:\n%q\nwant:\n%q", res.stdout, want)
	}
	if strings.Contains(res.stdout, "\x1b") {
		t.Error("piped stdout contains ANSI escapes")
	}
	if !strings.HasPrefix(res.stderr, "\ncombed") {
		t.Errorf("summary is not separated from findings: %q", res.stderr)
	}
	for _, wantErr := range []string{
		"combed 6 repositories",
		"4 need attention",
		"1 repository acknowledged",
	} {
		if !strings.Contains(res.stderr, wantErr) {
			t.Errorf("stderr missing %q: %q", wantErr, res.stderr)
		}
	}
}

func TestE2EShortPreservesCompactView(t *testing.T) {
	requireBinary(t)
	e2eEnv(t)
	base := buildKitchen(t)

	res := runBinary(t, "--short", base)
	if res.code != 1 {
		t.Fatalf("exit = %d, want 1; stderr: %s", res.code, res.stderr)
	}
	want := strings.Join([]string{
		row("D", "dirty"),
		row("L", "norem"),
		row("S", "stash"),
		row("U", "unpushed"),
	}, "\n") + "\n"
	if res.stdout != want {
		t.Errorf("stdout:\n%q\nwant:\n%q", res.stdout, want)
	}
}

func TestE2EDefaultDescribesTheUnpushedBranch(t *testing.T) {
	requireBinary(t)
	e2eEnv(t)
	base := buildKitchen(t)

	res := runBinary(t, base)
	if !strings.Contains(res.stdout, "keep/x  1 unpushed commit, no upstream") {
		t.Errorf("branch detail missing:\n%s", res.stdout)
	}
}

func TestE2ENamedOnlyFilterWorksProcessWide(t *testing.T) {
	requireBinary(t)
	e2eEnv(t)
	base := buildKitchen(t)

	res := runBinary(t, "--only-dirty", base)
	if res.code != 1 {
		t.Fatalf("exit = %d, want 1", res.code)
	}
	want := "Uncommitted changes:\n  dirty  [master]  1 untracked file\n"
	if res.stdout != want {
		t.Errorf("stdout:\n%q\nwant:\n%q", res.stdout, want)
	}
	if !strings.Contains(res.stderr, "1 needs attention") {
		t.Errorf("stderr: %q", res.stderr)
	}
}

func TestE2EExceptHidesTheNoise(t *testing.T) {
	requireBinary(t)
	e2eEnv(t)
	base := buildKitchen(t)

	res := runBinary(t, "-xL", base)
	if res.code != 1 {
		t.Fatalf("exit = %d, want 1", res.code)
	}
	if strings.Contains(res.stdout, "  norem\n") {
		t.Errorf("excluded L row still rendered:\n%s", res.stdout)
	}
	if !strings.Contains(res.stdout, "  dirty  [master]") {
		t.Errorf("unexcluded rows missing:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "3 need attention") {
		t.Errorf("stderr: %q", res.stderr)
	}
}

func TestE2ECleanTreeExitsZero(t *testing.T) {
	requireBinary(t)
	e2eEnv(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e2eSynced(t, filepath.Join(base, "clean"), filepath.Join(t.TempDir(), "clean.git"))

	res := runBinary(t, base)
	if res.code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", res.code, res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("stdout = %q, want empty", res.stdout)
	}
	if !strings.Contains(res.stderr, "combed 1 repository") {
		t.Errorf("stderr = %q", res.stderr)
	}
}

func TestE2EBrokenRepoExitsTwo(t *testing.T) {
	requireBinary(t)
	e2eEnv(t)
	base := t.TempDir()
	broken := filepath.Join(base, "broken")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, ".git"), []byte("gitdir: /nonexistent/nowhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := runBinary(t, base)
	if res.code != 2 {
		t.Fatalf("exit = %d, want 2; stderr: %s", res.code, res.stderr)
	}
	if !strings.HasPrefix(res.stdout, "Inspection failures:\n") {
		t.Errorf("stdout = %q, want a failure section", res.stdout)
	}
	if !strings.Contains(res.stderr, "1 failed") {
		t.Errorf("stderr = %q", res.stderr)
	}
}

func TestE2EVersionOnStdoutOnly(t *testing.T) {
	requireBinary(t)

	res := runBinary(t, "--version")
	if res.code != 0 {
		t.Fatalf("exit = %d, want 0", res.code)
	}
	if res.stdout != "git-comb dev\n" {
		t.Errorf("stdout = %q", res.stdout)
	}
	if res.stderr != "" {
		t.Errorf("stderr = %q, want empty", res.stderr)
	}
}

// TestE2EGitDispatch proves the subcommand contract itself: with the
// binary on PATH, plain `git comb` finds and runs it.
func TestE2EGitDispatch(t *testing.T) {
	requireBinary(t)
	e2eEnv(t)
	base := buildKitchen(t)

	env := os.Environ()
	pathSet := false
	newPath := "PATH=" + filepath.Dir(binPath) + string(os.PathListSeparator) + os.Getenv("PATH")
	for i, kv := range env {
		if strings.HasPrefix(strings.ToUpper(kv), "PATH=") {
			env[i] = newPath
			pathSet = true
		}
	}
	if !pathSet {
		env = append(env, newPath)
	}

	version := exec.Command("git", "comb", "--version")
	version.Env = env
	out, err := version.Output()
	if err != nil {
		t.Fatalf("git comb --version: %v", err)
	}
	if !strings.HasPrefix(string(out), "git-comb ") {
		t.Errorf("git comb --version = %q", out)
	}

	scan := exec.Command("git", "comb", base)
	scan.Env = env
	var stdout strings.Builder
	scan.Stdout = &stdout
	err = scan.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("git comb scan: err = %v, want exit 1", err)
	}
	if !strings.Contains(stdout.String(), "Branches:") ||
		!strings.Contains(stdout.String(), "\n  unpushed\n") {
		t.Errorf("dispatch scan output:\n%s", stdout.String())
	}
}
