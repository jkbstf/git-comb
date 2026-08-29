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

## Usage

```
git comb [OPTION]... [DIR]...
```

With no directories, the current directory is combed. Only
repositories needing attention are printed; each line is a sign
column, the repository path, and the checked-out branch.

| Option | Effect |
|---|---|
| `-f, --fetch` | fetch all remotes first, so behind is current |
| `-v, --verbose` | list the branches that hold unpushed commits |
| `-a, --all` | print clean repositories too |
| `--only SIGNS` | look only for these sign classes, e.g. `--only DUS` |
| `-j, --jobs N` | probe N repositories in parallel |
| `--hidden` | descend into hidden directories |
| `--prune NAME` | skip directories named NAME (repeatable) |
| `--color WHEN` | `auto` (default), `always`, or `never` |

| Sign | Meaning |
|---|---|
| `D` | uncommitted changes, untracked files included |
| `U` | commits that exist on no remote |
| `A` | ahead of upstream |
| `B` | behind upstream, as of the last fetch |
| `S` | stash entries |
| `E` | no commits yet |
| `N` | no remote configured |
| `R` | a remote could not be reached (with `--fetch`) |

The signs divide into loss risk — `D`, `U`, `S`, `E`, `N`, work that
exists nowhere else — and sync hygiene (`A`, `B`). `--only DUS` runs
a pure loss audit: classes you did not ask for are neither probed nor
printed, which also makes a narrow scan faster, and the exit status
follows what you asked for. Probe failures are always reported.

Exit status is 0 when everything is clean, 1 when something needs
attention, and 2 on errors — so the command slots directly into
scripts and shell prompts.

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
