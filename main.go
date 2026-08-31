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
	"strings"

	"github.com/jkbstf/git-comb/internal/comb"
)

// version is stamped by the release build; "dev" otherwise.
var version = "dev"

const usageFmt = `Usage: git comb [OPTION]... [DIR]...

Comb every Git repository under the given directories (default: the
current directory) and report work that exists only on this machine:
uncommitted changes, commits unreachable from any remote, and stashes.

    -v, --verbose     list the branches that hold unpushed commits
    -a, --all         print clean repositories too
    -o, --only SIGNS  look only for these sign classes (e.g. DUS)
    -x, --except SIGNS  look for everything but these classes (e.g. AB)
    -j, --jobs N      probe N repositories in parallel (default %d)
        --fetch       fetch all remotes first (may prompt), so behind
                      is current
        --hidden      descend into hidden directories
        --prune GLOB  skip directories matching GLOB (repeatable;
                      node_modules is always skipped)
        --no-ignores  disregard comb.ignore and comb.ignoreBranch
        --color WHEN  color the output: auto, always, never
        --version     print the version and exit
    -h, --help        show this help

Signs: D dirty  U unpushed  A ahead  B behind  S stashed
       E empty  L local  O offline

Defaults come from git config (comb.prune, comb.jobs, comb.hidden,
comb.only, comb.except); comb.ignore and comb.ignoreBranch
acknowledge repositories and branches per clone or globally. Flags
win key by key, only and except compose, and the summary discloses
acknowledgments and any narrowed selection.

Exit status: 0 all clean, 1 findings, 2 errors.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the whole program behind main, kept separate so tests can
// drive it with their own streams and read the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	var (
		opts        comb.Options
		colorWhen   string
		onlySigns   string
		exceptSigns string
		showVersion bool
	)

	fs := flag.NewFlagSet("git-comb", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.BoolVar(&opts.Fetch, "fetch", false, "")
	fs.BoolVar(&opts.Verbose, "verbose", false, "")
	fs.BoolVar(&opts.Verbose, "v", false, "")
	fs.BoolVar(&opts.All, "all", false, "")
	fs.BoolVar(&opts.All, "a", false, "")
	fs.IntVar(&opts.Jobs, "jobs", comb.DefaultJobs(), "")
	fs.IntVar(&opts.Jobs, "j", comb.DefaultJobs(), "")
	fs.BoolVar(&opts.Hidden, "hidden", false, "")
	fs.Var(&opts.Prune, "prune", "")
	fs.StringVar(&onlySigns, "only", "", "")
	fs.StringVar(&onlySigns, "o", "", "")
	fs.StringVar(&exceptSigns, "except", "", "")
	fs.StringVar(&exceptSigns, "x", "", "")
	fs.BoolVar(&opts.NoIgnores, "no-ignores", false, "")
	fs.StringVar(&colorWhen, "color", "auto", "")
	fs.BoolVar(&showVersion, "version", false, "")

	roots, err := parseArgs(fs, args)
	if err != nil {
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

	opts.Roots = roots
	if len(opts.Roots) == 0 {
		opts.Roots = []string{"."}
	}

	// Scan defaults come from git config; explicitly set flags win,
	// and prune values merge rather than replace.
	settings, err := comb.LoadSettings(".")
	if err != nil {
		fmt.Fprintf(stderr, "git-comb: config: %v\n", err)
		return 2
	}
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	if settings.Jobs > 0 && !visited["j"] && !visited["jobs"] {
		opts.Jobs = settings.Jobs
	}
	if settings.Hidden && !visited["hidden"] {
		opts.Hidden = true
	}
	opts.Prune = append(comb.PruneList(settings.Prune), opts.Prune...)

	// The selection composes: only chooses (flag over comb.only, else
	// everything), then except subtracts (flag over comb.except). An
	// empty result finds nothing.
	onlySrc, onlyStr := "--only", onlySigns
	if onlyStr == "" {
		onlySrc, onlyStr = "comb.only", settings.Only
	}
	if onlyStr != "" {
		opts.Only, err = comb.ParseSignSet(onlyStr)
		if err != nil {
			fmt.Fprintf(stderr, "git-comb: %s: %v\n", onlySrc, err)
			return 2
		}
	}
	exceptSrc, exceptStr := "--except", exceptSigns
	if exceptStr == "" {
		exceptSrc, exceptStr = "comb.except", settings.Except
	}
	if exceptStr != "" {
		hidden, err := comb.ParseSignSet(exceptStr)
		if err != nil {
			fmt.Fprintf(stderr, "git-comb: %s: %v\n", exceptSrc, err)
			return 2
		}
		opts.Only = opts.Only.Minus(hidden)
	}

	reports, err := comb.Run(opts)
	if err != nil {
		fmt.Fprintf(stderr, "git-comb: %v\n", err)
		return 2
	}
	attention, failed := comb.Render(stdout, reports, comb.RenderOptions{
		All:     opts.All,
		Verbose: opts.Verbose,
		Color:   useColor,
		Only:    opts.Only,
	})
	ackedRepos, ackedBranches := 0, 0
	for _, r := range reports {
		if r.Ignored {
			ackedRepos++
		}
		ackedBranches += r.AckedBranches
	}
	signsLabel := ""
	if !opts.Only.All() {
		signsLabel = opts.Only.String()
		if signsLabel == "" {
			signsLabel = "none"
		}
	}
	fmt.Fprintln(stderr, summary(len(reports), attention, failed, ackedRepos, ackedBranches, signsLabel))

	switch {
	case failed > 0:
		return 2
	case attention > 0:
		return 1
	}
	return 0
}

// parseArgs parses flags the way git commands do: options and
// directories may be interleaved, single-letter boolean flags
// combine ("-fv"), -j takes an attached value ("-j4"), and everything
// after "--" is a directory.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	head := args
	var tail []string
	for i, a := range args {
		if a == "--" {
			head, tail = args[:i], args[i+1:]
			break
		}
	}

	var roots []string
	rest := expandShortFlags(head)
	for {
		if err := fs.Parse(rest); err != nil {
			return nil, err
		}
		rest = fs.Args()
		if len(rest) == 0 {
			break
		}
		roots = append(roots, rest[0])
		rest = rest[1:]
	}
	return append(roots, tail...), nil
}

// expandShortFlags rewrites "-va" into "-v -a" and attached values
// like "-j4" or "-oDUS" into "-j 4" and "-o DUS", which the standard
// flag package does not do on its own. Following getopt convention,
// the leftmost letter decides: a value-taking short consumes the rest
// of the token as its argument.
func expandShortFlags(args []string) []string {
	const (
		boolShorts  = "va"
		valueShorts = "jox"
	)
	out := make([]string, 0, len(args))
	for _, a := range args {
		if len(a) < 3 || a[0] != '-' || a[1] == '-' {
			out = append(out, a)
			continue
		}
		body := a[1:]
		switch {
		case strings.IndexByte(valueShorts, body[0]) >= 0:
			out = append(out, a[:2], body[1:])
		case strings.Trim(body, boolShorts) == "":
			for _, c := range body {
				out = append(out, "-"+string(c))
			}
		default:
			out = append(out, a)
		}
	}
	return out
}

// summary is the one human line, written to stderr so stdout stays a
// pure finding list. Acknowledged repositories and branches — and any
// narrowed sign selection, however it was configured — are disclosed
// as qualifications: suppression asked for is focus, but it must never
// be silent.
func summary(repos, attention, failed, ackedRepos, ackedBranches int, signs string) string {
	noun := "repositories"
	if repos == 1 {
		noun = "repository"
	}
	verb := "need"
	if attention == 1 {
		verb = "needs"
	}
	s := fmt.Sprintf("combed %d %s: %d %s attention", repos, noun, attention, verb)
	if failed > 0 {
		s += fmt.Sprintf(", %d failed", failed)
	}
	var qualifications []string
	if ackedRepos > 0 || ackedBranches > 0 {
		var parts []string
		if ackedRepos > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", ackedRepos, pluralize(ackedRepos, "repository", "repositories")))
		}
		if ackedBranches > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", ackedBranches, pluralize(ackedBranches, "branch", "branches")))
		}
		qualifications = append(qualifications, strings.Join(parts, " and ")+" acknowledged")
	}
	if signs != "" {
		if signs == "none" {
			qualifications = append(qualifications, "no signs checked")
		} else {
			qualifications = append(qualifications, signs+" only")
		}
	}
	if len(qualifications) > 0 {
		s += " (" + strings.Join(qualifications, ", ") + ")"
	}
	return s
}

func pluralize(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// colorEnabled resolves the --color flag. Auto means: a terminal on
// stdout, NO_COLOR unset, and TERM not dumb.
func colorEnabled(when string, out io.Writer) (bool, error) {
	switch when {
	case "always":
		return true, nil
	case "never":
		return false, nil
	case "auto":
		if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
			return false, nil
		}
		f, ok := out.(*os.File)
		if !ok {
			return false, nil
		}
		info, err := f.Stat()
		if err != nil {
			return false, nil
		}
		return info.Mode()&os.ModeCharDevice != 0, nil
	}
	return false, fmt.Errorf("invalid --color value %q (want auto, always, or never)", when)
}
