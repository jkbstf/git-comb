# git-comb

Comb a directory tree for Git work that exists nowhere else.

`git comb` walks every repository under a directory and reports what a
routine `git status` habit misses: commits on branches that were never
pushed anywhere, uncommitted and untracked files, stashes, work left on
a detached HEAD, and ahead/behind state across every local branch.

```
$ git comb ~/Projects
Uncommitted changes:
  website  [main]  2 files changed: +5/-1, 1 untracked file

Branches:
  website
      feature/homepage                 2 unpushed commits, no upstream
  tools/scanner
    * main              [origin/main]  1 unpushed commit

Stashes:
  paperwork  1 stash

combed 25 repositories: 3 need attention
```

## Why another status tool

Most multi-repository tools model "unpushed" as "ahead of upstream".
A branch that was never pushed has no upstream, so it is ahead of
nothing, and the work most at risk of living on a single disk is
reported as clean. `git comb` asks a stricter question: which commits
are reachable from HEAD or any local branch, but from no
remote-tracking ref?

```
git rev-list --count HEAD --branches --not --remotes
```

That needs no upstream configuration and no network, and it holds for
every ref storage format, packed refs included, because git itself
answers the question.

## Install

```
go install github.com/jkbstf/git-comb@latest
```

or download a binary from the releases page and put it on `PATH`. Git
runs any executable named `git-comb` as `git comb`.

Requires git 2.31 or newer on `PATH`.

### Shell completion

The scripts under `contrib/completion` complete both `git comb` and
`git-comb`. For a local checkout, add the matching setup to your shell
configuration, using the checkout's absolute path.

Bash (`.bashrc`; Git completion must already be active):

```bash
source /path/to/git-comb/contrib/completion/git-comb-completion.bash
```

Zsh (`.zshrc`):

```zsh
autoload -Uz compinit && compinit
source /path/to/git-comb/contrib/completion/git-comb-completion.zsh
```

Fish (`~/.config/fish/config.fish`):

```fish
source /path/to/git-comb/contrib/completion/git-comb.fish
```

Restart the shell after saving the change. Release archives contain the same
scripts in their `completions` directory.

## Running from source

```
git clone https://github.com/jkbstf/git-comb.git
cd git-comb
go run . ~/projects
```

`go run` compiles whatever is on disk and passes the exit status
through, but note that it prints its own `exit status 1` line to
stderr whenever findings exist; this is the tool's normal result.
For noise-free runs build the binary instead (it is gitignored):

```
go build -o git-comb .
./git-comb ~/projects
```

To keep the `git comb` spelling while working from source, use a git
alias. Git resolves aliases before searching `PATH`, so this shadows
an installed `git-comb` until the alias is removed:

```
git config --global alias.comb '!go run /path/to/git-comb'
```

Or run it without cloning at all:

```
go run github.com/jkbstf/git-comb@latest ~/projects
```

Tests run with plain `go test ./...`; they build throwaway fixture
repositories in temporary directories and skip when git is absent.

## Usage

```
git comb [OPTION]... [DIR]...
```

With no directories, the current directory is combed. Only
repositories needing attention are printed. The default view groups
worktree and repository findings by state, while `U`, `A`, and `B` are
combined into one branch-oriented section. Each branch appears there
once, with its configured upstream and a concise description of what
needs attention. A repository can still appear in more than one
section, while the summary counts it once. Paths are relative to the
nearest directory passed for scanning. Use `--short` for the compact
sign view with one line per repository.

Dirty repositories include a `git diff --shortstat`-style summary of
tracked changes. Untracked files are counted separately in the same
summary because Git diffs normally omit them. Names, ref context in
square brackets, and quantitative details occupy separate aligned
columns within each section. Like Git's diffstat, the grouped view uses
the detected terminal width and falls back to 80 columns. Long paths
and refs are shortened independently in the middle so both ends stay
visible.

| Option | Effect |
|---|---|
| `--fetch` | fetch all remotes first, prompting if needed, so behind is current |
| `-s, --short` | show signs and paths, one repository per line |
| `-a, --all` | print clean repositories too |
| `--only-dirty` | look only for repositories with uncommitted changes |
| `--only-unpushed` | look only for commits that exist on no remote |
| `--only-ahead` | look only for branches ahead of their upstream |
| `--only-behind` | look only for branches behind their upstream |
| `--only-stashed` | look only for repositories with stashes |
| `--only-empty` | look only for empty repositories |
| `--only-local` | look only for repositories without remotes |
| `--only-offline` | look only for remotes unreachable during `--fetch` |
| `--exclude-dirty` | exclude repositories with uncommitted changes |
| `--exclude-unpushed` | exclude commits that exist on no remote |
| `--exclude-ahead` | exclude branches ahead of their upstream |
| `--exclude-behind` | exclude branches behind their upstream |
| `--exclude-stashed` | exclude repositories with stashes |
| `--exclude-empty` | exclude empty repositories |
| `--exclude-local` | exclude repositories without remotes |
| `--exclude-offline` | exclude remotes unreachable during `--fetch` |
| `-o, --only SIGNS` | advanced shorthand for combining sign classes, e.g. `-oDUS` |
| `-x, --except SIGNS` | exclude sign classes, e.g. `-xAB` |
| `-j, --jobs N` | probe N repositories in parallel |
| `--hidden` | descend into hidden directories |
| `--prune GLOB` | skip directories matching GLOB (repeatable) |
| `--no-ignores` | disregard `comb.ignore` and `comb.ignoreBranch` |
| `--color WHEN` | `auto` (default), `always`, or `never` |
| `--diagnostics FILE` | write privacy-safe performance diagnostics |

### Privacy-safe diagnostics

When a scan is unexpectedly slow, `--diagnostics FILE` writes a local JSON
Lines report that can be inspected or shared with an issue report:

```sh
git comb --diagnostics /tmp/git-comb-diagnostics.jsonl ~/Projects
```

In PowerShell:

```powershell
git comb --diagnostics "$env:TEMP\git-comb-diagnostics.jsonl" "$HOME\Projects"
```

Diagnostics are opt-in and are never uploaded. The file contains relative
timings, fixed Git operation categories, anonymous run-local repository IDs,
aggregate counts, concurrency, exit codes, the git-comb and Go versions, and
the operating system and architecture needed to compare performance.
Aggregate operational metadata such as repository and process counts is
therefore visible.

The report never contains filesystem paths, repository or branch names,
remotes or URLs, command arguments, command output, error messages,
environment values, user or host names, process IDs, absolute timestamps, or
machine identifiers. There is no option to include those values. The
diagnostic file is created with owner-only permissions where the platform
supports them, and an existing file is never overwritten.

The descriptive `--only-*` flags combine, so this scans only for
uncommitted changes, unpushed commits, and branches ahead of upstream:

```
git comb --only-dirty --only-unpushed --only-ahead ~/Projects
```

The matching `--exclude-*` flags also combine. This checks every state except
ahead and behind:

```
git comb --exclude-ahead --exclude-behind ~/Projects
```

Any command-line only filter replaces the configured only selection as a
unit. Any command-line exclude filter or `--except` likewise replaces the
configured exclusion selection; exclusions are then subtracted from the only
selection. Unselected classes are neither probed nor printed, which also makes
a narrow scan faster, and the exit status follows the resolved selection.
Whenever the selection is narrowed, the summary uses full state names:

```
combed 25 repositories, checking only uncommitted changes, unpushed commits, and branches ahead of upstream: 3 need attention
```

Probe failures are always reported.

### Short view and sign shorthand

`git comb --short` keeps the compact, grep-friendly view:

```
DU     website
S      paperwork
U      tools/scanner
```

The short view deliberately omits branch names: `U`, `A`, and `B` can
concern any local branch, not necessarily the one checked out in that
worktree. The grouped default lists every affected branch once. A
branch without an upstream is shown first; for tracked branches, square
brackets contain only the configured upstream. The final column
describes unpushed, ahead, and behind commits. When commits exist on no
remote, that actionable unpushed count replaces the overlapping ahead
count; behind remains visible independently. Ahead is shown when the
commits already exist on some remote but not on the configured upstream.
Following `git branch -vv`, `*` marks the branch checked out in this
worktree and `+` marks a branch checked out in another linked worktree.
Only the current `*` branch is green when color is enabled. Missing and
removed upstreams are described as `no upstream` and `upstream gone` in
the final column.

Every sign is the initial of a single word:

| Sign | Meaning |
|---|---|
| `D` | **dirty**: uncommitted changes, untracked files included |
| `U` | **unpushed**: commits that exist on no remote |
| `A` | **ahead**: at least one local branch is ahead of its upstream |
| `B` | **behind**: at least one local branch is behind its upstream, as of the last fetch |
| `S` | **stashed**: stash entries present |
| `E` | **empty**: no commits on any branch |
| `L` | **local**: no remote configured; the repository exists only here |
| `O` | **offline**: a remote could not be reached (with `--fetch`) |

The signs divide into loss risk (`D`, `U`, `S`, `E`, `L`: work that
exists nowhere else) and sync hygiene (`A`, `B`). The concise
`-oDUS` is equivalent to `--only-dirty --only-unpushed
--only-stashed`; `-xAB` is equivalent to `--exclude-ahead
--exclude-behind`. The forms can be combined, and a selection that ends empty
simply finds nothing.

Exit status is 0 when everything is clean, 1 when something needs
attention, and 2 on errors, so the command slots directly into
scripts and shell prompts.

## Configuration

Settings live in git config; there is no extra file, and they travel with
your dotfiles. `--prune` adds to `comb.prune` rather than replacing
it. The named `comb.only*` and `comb.exclude*` booleans build the configured
only and exclusion selections; compact `comb.only` and `comb.except` add sign
classes for advanced use. Command-line filters replace the configured
selection in their respective family, and exclusions are applied last.

| Key | Meaning |
|---|---|
| `comb.prune` (multi-valued) | directory-name globs to skip, like `--prune` |
| `comb.jobs` | default probe parallelism |
| `comb.hidden` | descend into hidden directories by default |
| `comb.onlyDirty` | include uncommitted changes in the standing only selection |
| `comb.onlyUnpushed` | include commits that exist on no remote |
| `comb.onlyAhead` | include branches ahead of their upstream |
| `comb.onlyBehind` | include branches behind their upstream |
| `comb.onlyStashed` | include repositories with stashes |
| `comb.onlyEmpty` | include empty repositories |
| `comb.onlyLocal` | include repositories without remotes |
| `comb.onlyOffline` | include remotes unreachable during `--fetch` |
| `comb.excludeDirty` | exclude repositories with uncommitted changes |
| `comb.excludeUnpushed` | exclude commits that exist on no remote |
| `comb.excludeAhead` | exclude branches ahead of their upstream |
| `comb.excludeBehind` | exclude branches behind their upstream |
| `comb.excludeStashed` | exclude repositories with stashes |
| `comb.excludeEmpty` | exclude empty repositories |
| `comb.excludeLocal` | exclude repositories without remotes |
| `comb.excludeOffline` | exclude remotes unreachable during `--fetch` |
| `comb.only` | advanced compact sign selection, like `--only` |
| `comb.except` | standing sign exclusion, like `--except` |
| `comb.ignore` | acknowledge this repository entirely |
| `comb.ignoreBranch` (multi-valued) | globs for branches whose unpushed commits are deliberate |

```
git config --global --add comb.prune _deps
git config --global --add comb.prune 'build*'
git config --global comb.onlyDirty true
git config --global comb.onlyUnpushed true
git config --global comb.onlyStashed true
git config --global comb.excludeAhead true
git config --global comb.excludeBehind true
git config comb.ignore true
git config --add comb.ignoreBranch 'backup/*'
git config --global --add comb.ignoreBranch 'wip/*'
```

`comb.ignore` and `comb.ignoreBranch` are read per repository with
git's usual precedence, so a global value applies everywhere and a
local one to a single clone. Branch globs and prune globs share one
grammar: they match the branch or directory name, and `*` does not
cross `/`, so `backup/*` matches `backup/a` but not `backup/a/b`.

Acknowledged findings are never silently gone: the summary line
counts them (`1 repository and 13 branches acknowledged`), and
`--no-ignores` shows the unfiltered truth.

## Design

- Every probe is a documented, stable git interface: `status
  --porcelain=v2`, `diff --numstat`, `rev-list`, `for-each-ref`,
  executed by the `git` on `PATH`. New repository formats keep working
  the day git ships them, which is not true of tools that read `.git`
  themselves.
- Read-only by construction: probes pass `--no-optional-locks`, so a
  scan never writes to a repository or races an editor for the index.
  The one exception is the opt-in `--fetch`, which runs once per
  repository (not once per worktree) and uses your configured
  authentication, including interactive credential, passphrase, and
  host-confirmation prompts. Fetches are serialized so prompts cannot
  overlap; local probes remain parallel. An unreachable remote is
  reported as `O`.
- Linked worktrees are recognized: commits on branches and stashes
  live in the shared ref store and are counted once per repository,
  at the primary worktree, or at one of the linked worktrees when the
  primary is outside the scanned tree. Each worktree still reports
  its own files and its own detached-HEAD commits.
- Probes ignore inherited `GIT_DIR`/`GIT_WORK_TREE`, so running from
  a git hook or an exported shell cannot redirect the scan.
- Hidden directories and `node_modules` are skipped during discovery;
  `--prune` extends the skip list, `--hidden` narrows it. Symlinked
  roots are resolved before walking.
- Repositories are probed in parallel; a repository that cannot be
  probed becomes a reported `!` finding (exit 2), never an aborted
  scan.
- No dependencies beyond the Go standard library.

## Notes

- Scanning goes downward from each directory: run `git comb` at the
  top of your projects tree, not inside one repository.
- Bare repositories (a directory that *is* a git dir, with no
  worktree) are not scanned.
- The grouped default gathers diff statistics for dirty repositories
  and exact local-only and upstream-divergence counts for affected
  branches. The short view skips those detailed counts. Branch
  histories may overlap, so per-branch local-only counts are not
  intended to be added together.
- The summary line goes to stderr; stdout carries grouped findings.
  With `--short`, stdout contains one repository per line.

## License

MIT; see [LICENSE](LICENSE).
