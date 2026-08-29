package comb

// Integration tests drive real git against fixture repositories. Each
// fixture encodes a case the tool exists to catch — several of them
// are exactly the cases where modeling "unpushed" as "ahead of
// upstream" reports clean.

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

// requireGit skips when no git binary is available.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
}

// setupGitEnv isolates every git invocation in this process from the
// user and system configuration, and pins the default branch name so
// fixtures behave identically on any machine.
func setupGitEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitconfig")
	content := "[user]\n\tname = Test\n\temail = test@example.invalid\n" +
		"[init]\n\tdefaultBranch = master\n" +
		"[commit]\n\tgpgsign = false\n"
	if err := os.WriteFile(cfg, []byte(content), 0o600); err != nil {
		t.Fatalf("write gitconfig: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
}

func mustGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := gitOut(repo, args...)
	if err != nil {
		t.Fatalf("git %v in %s: %v", args, repo, err)
	}
	return out
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	mustGit(t, dir, "init", "--quiet")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func commitFile(t *testing.T, repo, name, content, msg string) {
	t.Helper()
	writeFile(t, filepath.Join(repo, name), content)
	mustGit(t, repo, "add", "--all")
	mustGit(t, repo, "commit", "--quiet", "-m", msg)
}

// addRemote wires a local bare repository as origin and returns it.
func addRemote(t *testing.T, repo string) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "origin.git")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", bare, err)
	}
	mustGit(t, bare, "init", "--quiet", "--bare")
	mustGit(t, repo, "remote", "add", "origin", bare)
	return bare
}

// syncedRepo builds a repository with one pushed commit, tracking its
// origin — the baseline every scenario starts from.
func syncedRepo(t *testing.T) (repo, bare string) {
	t.Helper()
	repo = filepath.Join(t.TempDir(), "repo")
	initRepo(t, repo)
	commitFile(t, repo, "file.txt", "content\n", "first")
	bare = addRemote(t, repo)
	mustGit(t, repo, "push", "--quiet", "--set-upstream", "origin", "master")
	return repo, bare
}

func TestProbeCleanRepo(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, _ := syncedRepo(t)

	r := probe(repo, Options{})
	if r.Err != nil {
		t.Fatalf("probe: %v", r.Err)
	}
	if !r.Clean() {
		t.Errorf("want clean, got signs %q", r.Signs())
	}
	if r.Branch != "master" {
		t.Errorf("Branch = %q, want master", r.Branch)
	}
}

func TestProbeUntrackedOnlyIsDirty(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, _ := syncedRepo(t)
	writeFile(t, filepath.Join(repo, "untracked.txt"), "new\n")

	r := probe(repo, Options{})
	if got, want := r.Signs(), "D"; got != want {
		t.Errorf("Signs() = %q, want %q", got, want)
	}
}

// TestProbeNeverPushedBranch is the reason this tool exists: the
// current branch is fully in sync with its upstream, and one commit
// sits on a side branch that has no upstream and is on no remote.
func TestProbeNeverPushedBranch(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, _ := syncedRepo(t)
	mustGit(t, repo, "checkout", "--quiet", "-b", "backup/important")
	commitFile(t, repo, "file.txt", "changed\n", "work to keep")
	mustGit(t, repo, "checkout", "--quiet", "master")

	r := probe(repo, Options{Verbose: true})
	if r.Err != nil {
		t.Fatalf("probe: %v", r.Err)
	}
	if got, want := r.Signs(), "U"; got != want {
		t.Errorf("Signs() = %q, want %q", got, want)
	}
	if r.Unpushed != 1 {
		t.Errorf("Unpushed = %d, want 1", r.Unpushed)
	}
	want := []BranchCount{{Name: "backup/important", Commits: 1}}
	if len(r.UnpushedBranches) != 1 || r.UnpushedBranches[0] != want[0] {
		t.Errorf("UnpushedBranches = %+v, want %+v", r.UnpushedBranches, want)
	}
}

// TestProbePackedRefsStillDetected guards against enumerating loose
// ref files: after git pack-refs the branch exists only in
// packed-refs, and the unpushed commit must still be found.
func TestProbePackedRefsStillDetected(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, _ := syncedRepo(t)
	mustGit(t, repo, "checkout", "--quiet", "-b", "backup/important")
	commitFile(t, repo, "file.txt", "changed\n", "work to keep")
	mustGit(t, repo, "checkout", "--quiet", "master")
	mustGit(t, repo, "pack-refs", "--all")

	r := probe(repo, Options{})
	if r.Unpushed != 1 {
		t.Errorf("Unpushed = %d after pack-refs, want 1", r.Unpushed)
	}
}

func TestProbeDetachedHeadCommit(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, _ := syncedRepo(t)
	mustGit(t, repo, "checkout", "--quiet", "--detach")
	commitFile(t, repo, "file.txt", "detached work\n", "detached commit")

	r := probe(repo, Options{Verbose: true})
	if r.Unpushed != 1 {
		t.Errorf("Unpushed = %d, want 1", r.Unpushed)
	}
	if len(r.Branch) < len("detached@") || r.Branch[:len("detached@")] != "detached@" {
		t.Errorf("Branch = %q, want detached@<oid>", r.Branch)
	}
	if len(r.UnpushedBranches) != 1 || r.UnpushedBranches[0].Name != "(detached)" {
		t.Errorf("UnpushedBranches = %+v, want one (detached) entry", r.UnpushedBranches)
	}
}

func TestProbeStashOnly(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, _ := syncedRepo(t)
	writeFile(t, filepath.Join(repo, "file.txt"), "modified\n")
	mustGit(t, repo, "stash", "--quiet")

	r := probe(repo, Options{})
	if got, want := r.Signs(), "S"; got != want {
		t.Errorf("Signs() = %q, want %q", got, want)
	}
	if r.Stashes != 1 {
		t.Errorf("Stashes = %d, want 1", r.Stashes)
	}
}

func TestProbeNoRemote(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo := filepath.Join(t.TempDir(), "repo")
	initRepo(t, repo)
	commitFile(t, repo, "file.txt", "content\n", "first")

	r := probe(repo, Options{})
	if got, want := r.Signs(), "N"; got != want {
		t.Errorf("Signs() = %q, want %q", got, want)
	}
	if r.Unpushed != 0 {
		t.Errorf("Unpushed = %d with no remote, want 0 (N carries the case)", r.Unpushed)
	}
}

func TestProbeEmptyRepo(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo := filepath.Join(t.TempDir(), "repo")
	initRepo(t, repo)

	r := probe(repo, Options{})
	if got, want := r.Signs(), "EN"; got != want {
		t.Errorf("Signs() = %q, want %q", got, want)
	}
}

// TestProbeAheadIsAlsoUnpushed: a commit ahead of upstream is by
// definition on no remote, so A implies U.
func TestProbeAheadIsAlsoUnpushed(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, _ := syncedRepo(t)
	commitFile(t, repo, "file.txt", "ahead\n", "not pushed yet")

	r := probe(repo, Options{})
	if got, want := r.Signs(), "UA"; got != want {
		t.Errorf("Signs() = %q, want %q", got, want)
	}
}

func TestProbeBehind(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, bare := syncedRepo(t)

	other := filepath.Join(t.TempDir(), "other")
	parent := filepath.Dir(other)
	mustGit(t, parent, "clone", "--quiet", bare, other)
	commitFile(t, other, "file.txt", "remote work\n", "pushed elsewhere")
	mustGit(t, other, "push", "--quiet")

	mustGit(t, repo, "fetch", "--quiet")
	r := probe(repo, Options{})
	if got, want := r.Signs(), "B"; got != want {
		t.Errorf("Signs() = %q, want %q", got, want)
	}
}

func TestProbeFetchUpdatesBehind(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, bare := syncedRepo(t)

	other := filepath.Join(t.TempDir(), "other")
	mustGit(t, filepath.Dir(other), "clone", "--quiet", bare, other)
	commitFile(t, other, "file.txt", "remote work\n", "pushed elsewhere")
	mustGit(t, other, "push", "--quiet")

	// Without a fetch the stale remote-tracking ref hides the truth.
	if r := probe(repo, Options{}); r.Behind {
		t.Fatal("Behind before any fetch; the fixture is broken")
	}
	if r := probe(repo, Options{Fetch: true}); !r.Behind {
		t.Error("Behind = false after probe with Fetch")
	}
}

func TestProbeFetchFailureIsReported(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, bare := syncedRepo(t)
	if err := os.RemoveAll(bare); err != nil {
		t.Fatalf("remove bare: %v", err)
	}

	r := probe(repo, Options{Fetch: true})
	if !r.FetchFailed {
		t.Error("FetchFailed = false with the remote gone")
	}
}

func TestProbeErrorOnBrokenRepo(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo := filepath.Join(t.TempDir(), "broken")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, ".git"), "gitdir: /nonexistent/nowhere\n")

	r := probe(repo, Options{})
	if r.Err == nil {
		t.Error("Err = nil for a broken gitdir pointer")
	}
}

// TestProbeLinkedWorktree: shared ref-store findings are counted at
// the primary worktree only, while tree state stays per-worktree.
func TestProbeLinkedWorktree(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, _ := syncedRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	mustGit(t, repo, "worktree", "add", "--quiet", "-b", "wt-branch", wt)
	commitFile(t, wt, "file.txt", "worktree work\n", "never pushed")

	primary := probe(repo, Options{})
	if primary.Linked {
		t.Error("primary worktree reported as linked")
	}
	if primary.Unpushed != 1 {
		t.Errorf("primary Unpushed = %d, want 1 (wt-branch commit)", primary.Unpushed)
	}

	linked := probe(wt, Options{})
	if !linked.Linked {
		t.Error("linked worktree not detected")
	}
	if linked.Unpushed != 0 || linked.Stashes != 0 {
		t.Errorf("linked worktree double-counts shared state: %+v", linked)
	}

	writeFile(t, filepath.Join(wt, "scratch.txt"), "dirt\n")
	if r := probe(wt, Options{}); !r.Dirty {
		t.Error("dirty linked worktree not reported dirty")
	}
}

func TestScanFindsNestedSkipsNoise(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	base := t.TempDir()
	for _, p := range []string{
		filepath.Join(base, "keep"),
		filepath.Join(base, "keep", "inner"),
		filepath.Join(base, "node_modules", "dep"),
		filepath.Join(base, ".hidden", "h"),
		filepath.Join(base, "vendor", "v"),
	} {
		initRepo(t, p)
	}

	got, err := Scan([]string{base}, false, []string{"vendor"})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	sort.Strings(got)
	want := []string{
		filepath.Join(base, "keep"),
		filepath.Join(base, "keep", "inner"),
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Scan = %v, want %v", got, want)
	}

	withHidden, err := Scan([]string{base}, true, []string{"vendor"})
	if err != nil {
		t.Fatalf("Scan hidden: %v", err)
	}
	if len(withHidden) != 3 {
		t.Errorf("Scan with hidden found %d repos, want 3: %v", len(withHidden), withHidden)
	}
}

func TestScanRootIsARepo(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	root := filepath.Join(t.TempDir(), "root")
	initRepo(t, root)

	got, err := Scan([]string{root}, false, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 1 || got[0] != root {
		t.Errorf("Scan = %v, want [%s]", got, root)
	}
}

func TestScanMissingRootFails(t *testing.T) {
	if _, err := Scan([]string{filepath.Join(t.TempDir(), "nope")}, false, nil); err == nil {
		t.Error("Scan of a missing root did not fail")
	}
}

func TestRunSortsReports(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	base := t.TempDir()
	for _, name := range []string{"zeta", "alpha", "mid"} {
		p := filepath.Join(base, name)
		initRepo(t, p)
		commitFile(t, p, "f.txt", "x\n", "first")
	}

	reports, err := Run(Options{Roots: []string{base}, Jobs: 4})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(reports) != 3 {
		t.Fatalf("Run found %d repos, want 3", len(reports))
	}
	for i := 1; i < len(reports); i++ {
		if reports[i-1].Path > reports[i].Path {
			t.Errorf("reports not sorted: %q before %q", reports[i-1].Path, reports[i].Path)
		}
	}
}
