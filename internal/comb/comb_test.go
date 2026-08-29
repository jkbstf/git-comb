package comb

// Integration tests drive real git against fixture repositories. Each
// fixture encodes a case the tool exists to catch — several of them
// are exactly the cases where modeling "unpushed" as "ahead of
// upstream" reports clean.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
)

// requireGit skips when no git binary is available.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
}

// tempDir is t.TempDir with symlinks resolved, so paths compare
// stably on platforms where the temp root is itself a symlink.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	return dir
}

// setupGitEnv isolates every git invocation in this process from the
// user and system configuration, and pins the default branch name so
// fixtures behave identically on any machine.
func setupGitEnv(t *testing.T) {
	t.Helper()
	dir := tempDir(t)
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
	bare := filepath.Join(tempDir(t), "origin.git")
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
	repo = filepath.Join(tempDir(t), "repo")
	initRepo(t, repo)
	commitFile(t, repo, "file.txt", "content\n", "first")
	bare = addRemote(t, repo)
	mustGit(t, repo, "push", "--quiet", "--set-upstream", "origin", "master")
	return repo, bare
}

// probeAlone probes a repository standing on its own: it is the
// carrier of its one-member group and not a linked worktree.
func probeAlone(repo string, opts Options) Report {
	return probe(repo, opts, true, false)
}

func TestProbeCleanRepo(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, _ := syncedRepo(t)

	r := probeAlone(repo, Options{})
	if r.Err != nil {
		t.Fatalf("probe: %v", r.Err)
	}
	if got := r.Signs(); got != "" {
		t.Errorf("want clean, got signs %q", got)
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

	r := probeAlone(repo, Options{})
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

	r := probeAlone(repo, Options{Verbose: true})
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
	if !slices.Equal(r.UnpushedBranches, want) {
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

	r := probeAlone(repo, Options{})
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

	r := probeAlone(repo, Options{Verbose: true})
	if r.Unpushed != 1 {
		t.Errorf("Unpushed = %d, want 1", r.Unpushed)
	}
	if !strings.HasPrefix(r.Branch, "detached@") {
		t.Errorf("Branch = %q, want detached@<oid>", r.Branch)
	}
	if len(r.UnpushedBranches) != 1 || r.UnpushedBranches[0].Name != "(detached)" {
		t.Errorf("UnpushedBranches = %+v, want one (detached) entry", r.UnpushedBranches)
	}
}

// TestProbeOrphanCheckoutKeepsUnpushedVisible: an unborn HEAD after
// git checkout --orphan is not an empty repository — the other
// branches can still hold unpushed work, and E must not swallow U.
func TestProbeOrphanCheckoutKeepsUnpushedVisible(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, _ := syncedRepo(t)
	commitFile(t, repo, "file.txt", "unpushed\n", "not pushed")
	mustGit(t, repo, "checkout", "--quiet", "--orphan", "wip-pages")

	r := probeAlone(repo, Options{Verbose: true})
	if r.Err != nil {
		t.Fatalf("probe: %v", r.Err)
	}
	if r.Empty {
		t.Error("Empty = true although master still holds commits")
	}
	if r.Unpushed != 1 {
		t.Errorf("Unpushed = %d, want 1", r.Unpushed)
	}
	if !strings.Contains(r.Signs(), "U") {
		t.Errorf("Signs() = %q, want it to contain U", r.Signs())
	}
	if r.Branch != "wip-pages" {
		t.Errorf("Branch = %q, want wip-pages", r.Branch)
	}
}

func TestProbeStashOnly(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, _ := syncedRepo(t)
	writeFile(t, filepath.Join(repo, "file.txt"), "modified\n")
	mustGit(t, repo, "stash", "--quiet")

	r := probeAlone(repo, Options{})
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
	repo := filepath.Join(tempDir(t), "repo")
	initRepo(t, repo)
	commitFile(t, repo, "file.txt", "content\n", "first")

	r := probeAlone(repo, Options{})
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
	repo := filepath.Join(tempDir(t), "repo")
	initRepo(t, repo)

	r := probeAlone(repo, Options{})
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

	r := probeAlone(repo, Options{})
	if got, want := r.Signs(), "UA"; got != want {
		t.Errorf("Signs() = %q, want %q", got, want)
	}
}

func TestProbeBehind(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, bare := syncedRepo(t)

	other := filepath.Join(tempDir(t), "other")
	mustGit(t, filepath.Dir(other), "clone", "--quiet", bare, other)
	commitFile(t, other, "file.txt", "remote work\n", "pushed elsewhere")
	mustGit(t, other, "push", "--quiet")

	mustGit(t, repo, "fetch", "--quiet")
	r := probeAlone(repo, Options{})
	if got, want := r.Signs(), "B"; got != want {
		t.Errorf("Signs() = %q, want %q", got, want)
	}
}

func TestProbeFetchUpdatesBehind(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, bare := syncedRepo(t)

	other := filepath.Join(tempDir(t), "other")
	mustGit(t, filepath.Dir(other), "clone", "--quiet", bare, other)
	commitFile(t, other, "file.txt", "remote work\n", "pushed elsewhere")
	mustGit(t, other, "push", "--quiet")

	// Without a fetch the stale remote-tracking ref hides the truth.
	if r := probeAlone(repo, Options{}); r.Behind {
		t.Fatal("Behind before any fetch; the fixture is broken")
	}
	if r := probeAlone(repo, Options{Fetch: true}); !r.Behind {
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

	if r := probeAlone(repo, Options{Fetch: true}); !r.FetchFailed {
		t.Error("FetchFailed = false with the remote gone")
	}
}

// TestProbeNonCarrierNeverFetches: the network cost belongs to the
// group's carrier; other worktrees of the same repository must not
// repeat it.
func TestProbeNonCarrierNeverFetches(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, bare := syncedRepo(t)
	if err := os.RemoveAll(bare); err != nil {
		t.Fatalf("remove bare: %v", err)
	}

	if r := probe(repo, Options{Fetch: true}, false, true); r.FetchFailed {
		t.Error("non-carrier probe fetched (FetchFailed set despite carrier=false)")
	}
}

func TestProbeErrorOnBrokenRepo(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo := filepath.Join(tempDir(t), "broken")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, ".git"), "gitdir: /nonexistent/nowhere\n")

	if r := probeAlone(repo, Options{}); r.Err == nil {
		t.Error("Err = nil for a broken gitdir pointer")
	}
}

// TestProbeIgnoresInheritedRepoLocation: GIT_DIR and friends in the
// parent environment (a git hook, a script) must not redirect probes
// at a different repository.
func TestProbeIgnoresInheritedRepoLocation(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	dirty, _ := syncedRepo(t)
	writeFile(t, filepath.Join(dirty, "untracked.txt"), "x\n")
	clean, _ := syncedRepo(t)

	t.Setenv("GIT_DIR", filepath.Join(dirty, ".git"))
	t.Setenv("GIT_WORK_TREE", dirty)

	r := probeAlone(clean, Options{})
	if r.Err != nil {
		t.Fatalf("probe: %v", r.Err)
	}
	if r.Dirty {
		t.Error("clean repository reported dirty: inherited GIT_DIR leaked into the probe")
	}
}

func TestGitEnvScrubsRepoLocation(t *testing.T) {
	t.Setenv("GIT_DIR", "/somewhere/.git")
	t.Setenv("GIT_WORK_TREE", "/somewhere")
	t.Setenv("GIT_CONFIG_GLOBAL", "/kept")

	env := gitEnv("GIT_TERMINAL_PROMPT=0")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "GIT_DIR=") || strings.Contains(joined, "GIT_WORK_TREE=") {
		t.Errorf("repo-location variables not scrubbed:\n%s", joined)
	}
	if !strings.Contains(joined, "GIT_CONFIG_GLOBAL=/kept") {
		t.Error("unrelated git variables must survive the scrub")
	}
	if env[len(env)-1] != "GIT_TERMINAL_PROMPT=0" {
		t.Error("extra variables must be appended")
	}
}

// TestRunLinkedWorktreeGroup: shared ref-store findings are counted
// exactly once, at the primary, while tree state stays per-worktree.
func TestRunLinkedWorktreeGroup(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, _ := syncedRepo(t)
	wt := filepath.Join(tempDir(t), "wt")
	mustGit(t, repo, "worktree", "add", "--quiet", "-b", "wt-branch", wt)
	commitFile(t, wt, "file.txt", "worktree work\n", "never pushed")
	writeFile(t, filepath.Join(wt, "scratch.txt"), "dirt\n")

	reports, err := Run(Options{Roots: []string{repo, wt}, Jobs: 4})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	byPath := map[string]Report{}
	for _, r := range reports {
		byPath[r.Path] = r
	}
	primary, linked := byPath[repo], byPath[wt]
	if primary.Linked {
		t.Error("primary worktree reported as linked")
	}
	if primary.Unpushed != 1 {
		t.Errorf("primary Unpushed = %d, want 1 (wt-branch commit)", primary.Unpushed)
	}
	if !linked.Linked {
		t.Error("linked worktree not detected")
	}
	if linked.Unpushed != 0 || linked.Stashes != 0 {
		t.Errorf("linked worktree double-counts shared state: %+v", linked)
	}
	if !linked.Dirty {
		t.Error("dirty linked worktree not reported dirty")
	}
}

// TestRunWorktreeWithoutPrimary: a linked worktree scanned without
// its primary must still report the repository's unpushed work —
// somebody has to carry it.
func TestRunWorktreeWithoutPrimary(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, _ := syncedRepo(t)
	wt := filepath.Join(tempDir(t), "wt")
	mustGit(t, repo, "worktree", "add", "--quiet", "-b", "wt-branch", wt)
	commitFile(t, wt, "file.txt", "worktree work\n", "never pushed")

	reports, err := Run(Options{Roots: []string{wt}, Jobs: 2})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("Run found %d repos, want 1", len(reports))
	}
	if reports[0].Unpushed != 1 {
		t.Errorf("Unpushed = %d for a worktree scanned without its primary, want 1", reports[0].Unpushed)
	}
}

// TestRunDetachedWorktreeCommit: a commit made on a detached HEAD in
// a linked worktree is on no branch, so the primary cannot see it —
// the worktree itself must report it, exactly once.
func TestRunDetachedWorktreeCommit(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, _ := syncedRepo(t)
	wt := filepath.Join(tempDir(t), "wt")
	mustGit(t, repo, "worktree", "add", "--quiet", "--detach", wt)
	commitFile(t, wt, "file.txt", "detached worktree work\n", "on no branch")

	reports, err := Run(Options{Roots: []string{repo, wt}, Jobs: 4})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	total := 0
	var wtReport Report
	for _, r := range reports {
		total += r.Unpushed
		if r.Path == wt {
			wtReport = r
		}
	}
	if total != 1 {
		t.Errorf("group total Unpushed = %d, want exactly 1", total)
	}
	if wtReport.Unpushed != 1 {
		t.Errorf("detached worktree Unpushed = %d, want 1", wtReport.Unpushed)
	}
}

// TestProbeOnlyGatesProbes: classes outside --only are not computed
// at all — the unpushed count stays untouched under --only D, and
// under --only U the untracked-file walk is skipped entirely (-uno).
func TestProbeOnlyGatesProbes(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, _ := syncedRepo(t)
	mustGit(t, repo, "checkout", "--quiet", "-b", "backup/x")
	commitFile(t, repo, "file.txt", "unpushed\n", "kept local")
	mustGit(t, repo, "checkout", "--quiet", "master")
	writeFile(t, filepath.Join(repo, "untracked.txt"), "new\n")
	writeFile(t, filepath.Join(repo, "file.txt"), "modified\n")
	mustGit(t, repo, "stash", "--quiet")

	onlyD, err := ParseSignSet("D")
	if err != nil {
		t.Fatal(err)
	}
	r := probe(repo, Options{Only: onlyD}, true, false)
	if r.Err != nil {
		t.Fatalf("probe: %v", r.Err)
	}
	if !r.Dirty {
		t.Error("Dirty = false with an untracked file under --only D")
	}
	if r.Unpushed != 0 || r.Stashes != 0 || r.NoRemote {
		t.Errorf("unrequested classes computed under --only D: %+v", r)
	}

	onlyU, err := ParseSignSet("U")
	if err != nil {
		t.Fatal(err)
	}
	r = probe(repo, Options{Only: onlyU}, true, false)
	if r.Err != nil {
		t.Fatalf("probe: %v", r.Err)
	}
	if r.Unpushed != 1 {
		t.Errorf("Unpushed = %d under --only U, want 1", r.Unpushed)
	}
	if r.Dirty {
		t.Error("untracked file registered although --only U runs status with -uno")
	}
	if r.Stashes != 0 {
		t.Error("stash counted under --only U")
	}
}

// TestRunOnlyDirtySkipsGrouping: with neither U nor S requested and
// no fetch, worktree grouping is unnecessary and skipped; tree state
// still reports correctly.
func TestRunOnlyDirtySkipsGrouping(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo, _ := syncedRepo(t)
	wt := filepath.Join(tempDir(t), "wt")
	mustGit(t, repo, "worktree", "add", "--quiet", "-b", "wt-branch", wt)
	commitFile(t, wt, "file.txt", "worktree work\n", "never pushed")
	writeFile(t, filepath.Join(wt, "scratch.txt"), "dirt\n")

	onlyD, err := ParseSignSet("D")
	if err != nil {
		t.Fatal(err)
	}
	reports, err := Run(Options{Roots: []string{repo, wt}, Jobs: 2, Only: onlyD})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, r := range reports {
		if r.Unpushed != 0 {
			t.Errorf("Unpushed computed under --only D: %+v", r)
		}
		if r.Path == wt && !r.Dirty {
			t.Error("dirty worktree missed under --only D")
		}
	}
}

func TestScanFindsNestedSkipsNoise(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	base := tempDir(t)
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
	if !slices.Equal(got, want) {
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
	root := filepath.Join(tempDir(t), "root")
	initRepo(t, root)

	got, err := Scan([]string{root}, false, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 1 || got[0] != root {
		t.Errorf("Scan = %v, want [%s]", got, root)
	}
}

// TestScanSymlinkedRoot: a symlinked root must scan what it points
// at; silently finding nothing would be a false all-clean.
func TestScanSymlinkedRoot(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	target := tempDir(t)
	initRepo(t, filepath.Join(target, "proj"))
	link := filepath.Join(tempDir(t), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	got, err := Scan([]string{link}, false, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Scan through symlinked root = %v, want 1 repo", got)
	}
	if runtime.GOOS != "windows" && got[0] != filepath.Join(target, "proj") {
		t.Errorf("Scan = %v, want the resolved path %s", got, filepath.Join(target, "proj"))
	}
}

// TestScanAliasedRootsDeduplicated: the same physical tree reached
// through a symlink alias must not be reported (or fetched) twice.
func TestScanAliasedRootsDeduplicated(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	target := tempDir(t)
	initRepo(t, filepath.Join(target, "proj"))
	link := filepath.Join(tempDir(t), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	got, err := Scan([]string{target, link}, false, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("Scan of aliased roots = %v, want 1 repo", got)
	}
}

func TestScanMissingRootFails(t *testing.T) {
	if _, err := Scan([]string{filepath.Join(t.TempDir(), "nope")}, false, nil); err == nil {
		t.Error("Scan of a missing root did not fail")
	}
}

// TestScanFileRootFails: a root that exists but is not a directory is
// a mistake, not an empty tree.
func TestScanFileRootFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "afile")
	writeFile(t, path, "not a directory\n")
	if _, err := Scan([]string{path}, false, nil); err == nil {
		t.Error("Scan of a file root did not fail")
	}
}

func TestRunSortsReports(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	base := tempDir(t)
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
	if !slices.IsSortedFunc(reports, func(a, b Report) int { return strings.Compare(a.Path, b.Path) }) {
		t.Errorf("reports not sorted by path: %+v", reports)
	}
}
