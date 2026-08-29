// Command git-comb combs a directory tree for Git repositories that
// hold work existing nowhere else: uncommitted changes, commits
// unreachable from any remote, and stashes. Installed on PATH it runs
// as `git comb`.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jkbstf/git-comb/internal/comb"
)

// version is stamped by the release build; "dev" otherwise.
var version = "dev"

const usageFmt = `Usage: git comb [OPTION]... [DIR]...

Comb every Git repository under the given directories (default: the
current directory) and report work that exists only on this machine:
uncommitted changes, commits unreachable from any remote, and stashes.

    -f, --fetch       fetch all remotes first, so behind is current
    -v, --verbose     list the branches that hold unpushed commits
    -a, --all         print clean repositories too
    -j, --jobs N      probe N repositories in parallel (default %d)
        --hidden      descend into hidden directories
        --prune NAME  skip directories named NAME (repeatable;
                      node_modules is always skipped)
        --color WHEN  color the output: auto, always, never
        --version     print the version and exit
    -h, --help        show this help

Signs: D dirty  U unpushed  A ahead  B behind  S stash
       E empty  N no remote  R remote unreachable

Exit status: 0 all clean, 1 findings, 2 errors.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the whole program behind main, kept separate so tests can
// drive it with their own streams and read the exit code.
func run(args []string, stdout, stderr *os.File) int {
	var (
		opts        comb.Options
		colorWhen   string
		showVersion bool
	)

	fs := flag.NewFlagSet("git-comb", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.BoolVar(&opts.Fetch, "fetch", false, "")
	fs.BoolVar(&opts.Fetch, "f", false, "")
	fs.BoolVar(&opts.Verbose, "verbose", false, "")
	fs.BoolVar(&opts.Verbose, "v", false, "")
	fs.BoolVar(&opts.All, "all", false, "")
	fs.BoolVar(&opts.All, "a", false, "")
	fs.IntVar(&opts.Jobs, "jobs", comb.DefaultJobs(), "")
	fs.IntVar(&opts.Jobs, "j", comb.DefaultJobs(), "")
	fs.BoolVar(&opts.Hidden, "hidden", false, "")
	fs.Var(&opts.Prune, "prune", "")
	fs.StringVar(&colorWhen, "color", "auto", "")
	fs.BoolVar(&showVersion, "version", false, "")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(stdout, usageFmt, comb.DefaultJobs())
			return 0
		}
		fmt.Fprintf(stderr, "git-comb: %v\n", err)
		fmt.Fprintln(stderr, "Try 'git comb --help' for more information.")
		return 2
	}
	if showVersion {
		fmt.Fprintln(stdout, "git-comb "+version)
		return 0
	}

	useColor, err := colorEnabled(colorWhen, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "git-comb: %v\n", err)
		return 2
	}

	opts.Roots = fs.Args()
	if len(opts.Roots) == 0 {
		opts.Roots = []string{"."}
	}

	start := time.Now()
	reports, err := comb.Run(opts)
	if err != nil {
		fmt.Fprintf(stderr, "git-comb: %v\n", err)
		return 2
	}
	attention, failed := comb.Render(stdout, reports, comb.RenderOptions{
		All:     opts.All,
		Verbose: opts.Verbose,
		Color:   useColor,
	})
	fmt.Fprintf(stderr, "combed %d repositories in %s: %d need attention\n",
		len(reports), time.Since(start).Round(time.Millisecond), attention)

	switch {
	case failed > 0:
		return 2
	case attention > 0:
		return 1
	}
	return 0
}

// colorEnabled resolves the --color flag. Auto means: a terminal on
// stdout, NO_COLOR unset, and TERM not dumb.
func colorEnabled(when string, out *os.File) (bool, error) {
	switch when {
	case "always":
		return true, nil
	case "never":
		return false, nil
	case "auto":
		if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
			return false, nil
		}
		info, err := out.Stat()
		if err != nil {
			return false, nil
		}
		return info.Mode()&os.ModeCharDevice != 0, nil
	}
	return false, fmt.Errorf("invalid --color value %q (want auto, always, or never)", when)
}
