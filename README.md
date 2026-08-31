# git-comb

Comb a directory tree for Git work that exists nowhere else.

`git comb` walks every repository under a directory and reports what a
routine `git status` habit misses: commits on branches that were never
pushed anywhere, uncommitted and untracked files, stashes, and work
left on a detached HEAD — across all repositories at once.

```
$ git comb ~/Projects
DU     /Users/js/Projects/website [main]
S      /Users/js/Projects/paperwork [master]
U      /Users/js/Projects/tools/scanner [main]
combed 25 repositories in 412ms: 3 need attention
```

## Why another status tool

Most multi-repository tools model "unpushed" as "ahead of upstream".
A branch that was never pushed has no upstream, so it is ahead of
nothing — and the work most at risk of living on a single disk is
reported as clean. `git comb` asks a stricter question: which commits
are reachable from HEAD or any local branch, but from no
remote-tracking ref?

```
git rev-list --count HEAD --branches --not --remotes
```

That needs no upstream configuration and no network, and it holds for
every ref storage format, packed refs included — because git itself
answers the question.

## Install

```
go install github.com/jkbstf/git-comb@latest
```

or download a binary from the releases page and put it on `PATH`. Git
runs any executable named `git-comb` as `git comb`.

Requires git 2.31 or newer on `PATH`.

## Running from source

```
git clone https://github.com/jkbstf/git-comb.git
cd git-comb
go run . ~/projects
```

`go run` compiles whatever is on disk and passes the exit status
through, but note that it prints its own `exit status 1` line to
stderr whenever findings exist — which is this tool's normal result.
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
repositories needing attention are printed; each line is a sign
column, the repository path, and the checked-out branch.

| Option | Effect |
|---|---|
| `--fetch` | fetch all remotes first, so behind is current |
| `-v, --verbose` | list the branches that hold unpushed commits |
| `-a, --all` | print clean repositories too |
| `-o, --only SIGNS` | look only for these sign classes, e.g. `-o DUS` |
| `-x, --except SIGNS` | look for everything but these classes, e.g. `-x AB` |
| `-j, --jobs N` | probe N repositories in parallel |
| `--hidden` | descend into hidden directories |
| `--prune GLOB` | skip directories matching GLOB (repeatable) |
| `--no-ignores` | disregard `comb.ignore` and `comb.ignoreBranch` |
| `--color WHEN` | `auto` (default), `always`, or `never` |

Every sign is the initial of a single word:

| Sign | Meaning |
|---|---|
| `D` | **dirty** — uncommitted changes, untracked files included |
| `U` | **unpushed** — commits that exist on no remote |
| `A` | **ahead** — of its upstream |
| `B` | **behind** — its upstream, as of the last fetch |
| `S` | **stashed** — stash entries present |
| `E` | **empty** — no commits on any branch |
| `L` | **local** — no remote configured; the repository exists only here |
| `O` | **offline** — a remote could not be reached (with `--fetch`) |

The signs divide into loss risk — `D`, `U`, `S`, `E`, `L`, work that
exists nowhere else — and sync hygiene (`A`, `B`). `--only DUS` runs
a pure loss audit; `--except AB` says the same thing by naming the
noise instead, which is often easier. The two compose: only chooses,
except then subtracts, and a selection that ends empty simply finds
nothing. Classes you did not ask for are neither probed nor printed,
which also makes a narrow scan faster, and the exit status follows
what you asked for. Whenever the selection is narrowed — by flag or
by config — the summary line notes it (`; signs: DU`). Probe
failures are always reported.

Exit status is 0 when everything is clean, 1 when something needs
attention, and 2 on errors — so the command slots directly into
scripts and shell prompts.

## Configuration

Settings live in git config — no extra file, and they travel with
your dotfiles. Flags override their matching keys; `--prune` adds to
`comb.prune` rather than replacing it, and the sign filters compose
after each side is resolved: `comb.only` narrowed by `--except`, or
any other pairing.

| Key | Meaning |
|---|---|
| `comb.prune` (multi-valued) | directory-name globs to skip, like `--prune` |
| `comb.jobs` | default probe parallelism |
| `comb.hidden` | descend into hidden directories by default |
| `comb.only` | standing sign selection, like `--only` |
| `comb.except` | standing sign exclusion, like `--except` |
| `comb.ignore` | acknowledge this repository entirely |
| `comb.ignoreBranch` (multi-valued) | globs for branches whose unpushed commits are deliberate |

```
git config --global --add comb.prune _deps
git config --global --add comb.prune 'build*'
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
counts them — `; acknowledged: 1 repository, 13 branches` — and
`--no-ignores` shows the unfiltered truth.

## Design

- Every probe is a documented, stable git interface — `status
  --porcelain=v2`, `rev-list`, `for-each-ref` — executed by the `git`
  on `PATH`. New repository formats keep working the day git ships
  them, which is not true of tools that read `.git` themselves.
- Read-only by construction: probes pass `--no-optional-locks`, so a
  scan never writes to a repository or races an editor for the index.
  The one exception is the opt-in `--fetch`, which runs once per
  repository (not once per worktree), uses your configured credential
  helpers, and never prompts on the terminal — an unreachable remote
  is reported as `R`, not a hang.
- Linked worktrees are recognized: commits on branches and stashes
  live in the shared ref store and are counted once per repository —
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
- `--verbose` runs one `rev-list` per local branch in repositories
  with unpushed work, so it costs more on branch-heavy repositories.
- The summary line goes to stderr; stdout carries only findings, one
  repository per line.

## License

MIT — see [LICENSE](LICENSE).
