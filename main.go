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
	"strconv"
	"strings"
	"time"

	"github.com/jkbstf/git-comb/internal/comb"
)

// version is stamped by the release build; "dev" otherwise.
var version = "dev"

const usageFmt = `Usage: git comb [OPTION]... [DIR]...

Comb every Git repository under the given directories (default: the
current directory) and report local work at risk plus synchronization
state across every local branch: uncommitted changes, commits
unreachable from any remote, stashes, and ahead/behind branches.

    -s, --short          show signs and paths, one repository per line
    -a, --all            print clean repositories too
        --only-dirty     look only for repositories with uncommitted changes
        --only-unpushed  look only for commits that exist on no remote
        --only-ahead     look only for branches ahead of their upstream
        --only-behind    look only for branches behind their upstream
        --only-stashed   look only for repositories with stashes
        --only-empty     look only for empty repositories
        --only-local     look only for repositories without remotes
        --only-offline   look only for remotes unreachable during --fetch
        --exclude-dirty   exclude repositories with uncommitted changes
        --exclude-unpushed exclude commits that exist on no remote
        --exclude-ahead   exclude branches ahead of their upstream
        --exclude-behind  exclude branches behind their upstream
        --exclude-stashed exclude repositories with stashes
        --exclude-empty   exclude empty repositories
        --exclude-local   exclude repositories without remotes
        --exclude-offline exclude remotes unreachable during --fetch
    -o, --only SIGNS     advanced shorthand for combining sign classes
    -x, --except SIGNS   exclude sign classes (e.g. AB)
    -j, --jobs N         probe N repositories in parallel (default %d)
        --fetch          fetch all remotes first (may prompt), so behind
                         is current
        --hidden         descend into hidden and system directories
        --max-depth N    descend at most N directories below each root
                         (root is depth 0; default unlimited)
        --prune GLOB     skip directories matching GLOB (repeatable;
                         node_modules is always skipped)
        --no-ignores     disregard comb.ignore and comb.ignoreBranch
        --color WHEN     color the output: auto, always, never
        --diagnostics FILE
                         write privacy-safe performance diagnostics
        --version        print the version and exit
    -h, --help           show this help

Signs: D dirty  U unpushed  A ahead  B behind  S stashed
       E empty  L local  O offline

Defaults come from git config (comb.prune, comb.jobs, comb.hidden,
comb.onlyDirty, comb.excludeDirty, the other named filters, comb.only,
comb.except); comb.ignore and comb.ignoreBranch acknowledge repositories
and branches per clone or globally. Command-line only and exclude filters
replace their configured family; named filters combine, exclusions then
subtract, and the summary discloses acknowledgments and narrowed selections.
--max-depth is command-line-only so future scans cannot be silently limited.

Exit status: 0 all clean, 1 findings, 2 errors.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the whole program behind main, kept separate so tests can
// drive it with their own streams and read the exit code.
func run(args []string, stdout, stderr io.Writer) (code int) {
	var (
		opts            comb.Options
		short           bool
		colorWhen       string
		diagnosticsPath string
		onlySigns       string
		exceptSigns     string
		onlyNamed       namedFilterFlags
		excludeNamed    namedFilterFlags
		maxDepth        = -1
		showVersion     bool
	)

	fs := flag.NewFlagSet("git-comb", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.BoolVar(&opts.Fetch, "fetch", false, "")
	fs.BoolVar(&short, "short", false, "")
	fs.BoolVar(&short, "s", false, "")
	fs.BoolVar(&opts.All, "all", false, "")
	fs.BoolVar(&opts.All, "a", false, "")
	fs.IntVar(&opts.Jobs, "jobs", comb.DefaultJobs(), "")
	fs.IntVar(&opts.Jobs, "j", comb.DefaultJobs(), "")
	fs.BoolVar(&opts.Hidden, "hidden", false, "")
	fs.IntVar(&maxDepth, "max-depth", -1, "")
	fs.Var(&opts.Prune, "prune", "")
	fs.StringVar(&onlySigns, "only", "", "")
	fs.StringVar(&onlySigns, "o", "", "")
	fs.StringVar(&exceptSigns, "except", "", "")
	fs.StringVar(&exceptSigns, "x", "", "")
	fs.BoolVar(&onlyNamed.Dirty, "only-dirty", false, "")
	fs.BoolVar(&onlyNamed.Unpushed, "only-unpushed", false, "")
	fs.BoolVar(&onlyNamed.Ahead, "only-ahead", false, "")
	fs.BoolVar(&onlyNamed.Behind, "only-behind", false, "")
	fs.BoolVar(&onlyNamed.Stashed, "only-stashed", false, "")
	fs.BoolVar(&onlyNamed.Empty, "only-empty", false, "")
	fs.BoolVar(&onlyNamed.Local, "only-local", false, "")
	fs.BoolVar(&onlyNamed.Offline, "only-offline", false, "")
	fs.BoolVar(&excludeNamed.Dirty, "exclude-dirty", false, "")
	fs.BoolVar(&excludeNamed.Unpushed, "exclude-unpushed", false, "")
	fs.BoolVar(&excludeNamed.Ahead, "exclude-ahead", false, "")
	fs.BoolVar(&excludeNamed.Behind, "exclude-behind", false, "")
	fs.BoolVar(&excludeNamed.Stashed, "exclude-stashed", false, "")
	fs.BoolVar(&excludeNamed.Empty, "exclude-empty", false, "")
	fs.BoolVar(&excludeNamed.Local, "exclude-local", false, "")
	fs.BoolVar(&excludeNamed.Offline, "exclude-offline", false, "")
	fs.BoolVar(&opts.NoIgnores, "no-ignores", false, "")
	fs.StringVar(&colorWhen, "color", "auto", "")
	fs.StringVar(&diagnosticsPath, "diagnostics", "", "")
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
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	if visited["max-depth"] {
		if maxDepth < 0 {
			fmt.Fprintln(stderr, "git-comb: --max-depth must be zero or greater")
			return 2
		}
		opts.MaxDepth = &maxDepth
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
	// The detailed view combines per-branch local-only and upstream
	// status and summarizes working-tree changes. The compact view
	// deliberately avoids those extra detail probes.
	opts.BranchDetails = !short
	opts.DirtyDetails = !short

	var diagnosticsFile *os.File
	if diagnosticsPath != "" {
		diagnosticsFile, err = os.OpenFile(diagnosticsPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			fmt.Fprintf(stderr, "git-comb: diagnostics: %v\n", err)
			return 2
		}
		opts.Diagnostics, err = comb.NewDiagnostics(diagnosticsFile, version)
		if err != nil {
			_ = diagnosticsFile.Close()
			fmt.Fprintf(stderr, "git-comb: diagnostics: %v\n", err)
			return 2
		}
		defer func() {
			if opts.Diagnostics.Err() != nil {
				code = 2
			}
			if err := opts.Diagnostics.Finish(code); err != nil {
				fmt.Fprintf(stderr, "git-comb: diagnostics: %v\n", err)
				code = 2
			}
			if err := diagnosticsFile.Close(); err != nil {
				fmt.Fprintf(stderr, "git-comb: diagnostics: %v\n", err)
				code = 2
			}
		}()
	}

	// Scan defaults come from git config; explicitly set flags win,
	// and prune values merge rather than replace.
	var configStarted time.Time
	if opts.Diagnostics != nil {
		configStarted = time.Now()
	}
	settings, err := comb.LoadSettingsWithDiagnostics(".", opts.Diagnostics)
	if opts.Diagnostics != nil {
		opts.Diagnostics.Phase("config", configStarted, nil)
	}
	if err != nil {
		fmt.Fprintf(stderr, "git-comb: config: %v\n", err)
		return 2
	}
	if settings.Jobs > 0 && !visited["j"] && !visited["jobs"] {
		opts.Jobs = settings.Jobs
	}
	if settings.Hidden && !visited["hidden"] {
		opts.Hidden = true
	}
	opts.Prune = append(comb.PruneList(settings.Prune), opts.Prune...)

	// Named only filters combine with each other and with the compact
	// --only shorthand. Any command-line only filter replaces the
	// configured only selection as a unit. Named exclude filters follow
	// the same rule for configured exclusions; exclusions then subtract.
	// An empty result finds nothing.
	cliOnly := onlySigns + onlyNamed.signs()
	onlySrc, onlyStr := "--only", cliOnly
	if cliOnly == "" {
		onlySrc, onlyStr = "comb.only", settings.Only+settings.OnlyNamed
	}
	if onlyStr != "" {
		opts.Only, err = comb.ParseSignSet(onlyStr)
		if err != nil {
			fmt.Fprintf(stderr, "git-comb: %s: %v\n", onlySrc, err)
			return 2
		}
	}
	cliExclude := exceptSigns + excludeNamed.signs()
	exceptSrc, exceptStr := "--except", cliExclude
	if cliExclude == "" {
		exceptSrc, exceptStr = "comb.except", settings.Except+settings.ExcludeNamed
	}
	if exceptStr != "" {
		hidden, err := comb.ParseSignSet(exceptStr)
		if err != nil {
			fmt.Fprintf(stderr, "git-comb: %s: %v\n", exceptSrc, err)
			return 2
		}
		opts.Only = opts.Only.Minus(hidden)
	}
	if opts.Diagnostics != nil {
		opts.Diagnostics.Options(comb.DiagnosticOptions{
			Roots: len(opts.Roots), Jobs: opts.Jobs, Prunes: len(opts.Prune),
			Selection: opts.Only.String(), Short: short, All: opts.All,
			Fetch: opts.Fetch, Hidden: opts.Hidden, NoIgnores: opts.NoIgnores,
			MaxDepth: opts.MaxDepth,
		})
	}

	renderOpts := comb.RenderOptions{
		Roots: opts.Roots,
		All:   opts.All,
		Short: short,
		Color: useColor,
		Width: outputWidth(stdout),
		Only:  opts.Only,
	}
	progress := newTerminalProgress(stderr, opts.Roots)
	defer progress.Stop()
	if progress != nil {
		opts.Progress = progress.Update
	}
	attention, failed := 0, 0
	wroteReport := false
	var renderDuration time.Duration
	opts.Report = func(report comb.Report) {
		started := time.Now()
		defer func() { renderDuration += time.Since(started) }()
		var block strings.Builder
		reportAttention, reportFailed := comb.Render(&block, []comb.Report{report}, renderOpts)
		attention += reportAttention
		failed += reportFailed
		if block.Len() == 0 {
			return
		}
		comb.WithTerminal(func() {
			progress.Suspend(func() {
				if wroteReport && !short {
					fmt.Fprintln(stdout)
				}
				_, _ = io.WriteString(stdout, block.String())
			})
		})
		wroteReport = true
	}

	reports, err := comb.Run(opts)
	progress.Stop()
	if err != nil {
		fmt.Fprintf(stderr, "git-comb: %v\n", err)
		return 2
	}
	if opts.Diagnostics != nil {
		opts.Diagnostics.PhaseDuration("rendering", renderDuration, map[string]int{"repositories": len(reports)})
	}
	ackedRepos, ackedBranches := 0, 0
	for _, r := range reports {
		if r.Ignored {
			ackedRepos++
		}
		ackedBranches += r.AckedBranches
	}
	selectionLabel := ""
	if !opts.Only.All() {
		selectionLabel = selectedStates(opts.Only)
	}
	if wroteReport {
		fmt.Fprintln(stderr)
	}
	fmt.Fprintln(stderr, summary(len(reports), attention, failed, ackedRepos, ackedBranches, selectionLabel, opts.MaxDepth))

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
// combine ("-sa"), -j takes an attached value ("-j4"), and everything
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

// expandShortFlags rewrites "-sa" into "-s -a" and attached values
// like "-j4" or "-oDUS" into "-j 4" and "-o DUS", which the standard
// flag package does not do on its own. Following getopt convention,
// the leftmost letter decides: a value-taking short consumes the rest
// of the token as its argument.
func expandShortFlags(args []string) []string {
	const (
		boolShorts  = "sa"
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
// pure finding list. A narrowed selection is integrated into the main
// sentence using state names; the sign vocabulary stays an optional
// shorthand rather than the primary interface.
func summary(repos, attention, failed, ackedRepos, ackedBranches int, selection string, maxDepth *int) string {
	noun := "repositories"
	if repos == 1 {
		noun = "repository"
	}
	verb := "need"
	if attention == 1 {
		verb = "needs"
	}
	s := fmt.Sprintf("combed %d %s", repos, noun)
	if selection == "none" {
		s += " without checking any states"
	} else if selection != "" {
		s += ", checking only " + selection
	}
	s += fmt.Sprintf(": %d %s attention", attention, verb)
	if failed > 0 {
		s += fmt.Sprintf(", %d failed", failed)
	}
	var qualifications []string
	if maxDepth != nil {
		qualifications = append(qualifications, fmt.Sprintf("scan limited to depth %d", *maxDepth))
	}
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
	if len(qualifications) > 0 {
		s += " (" + strings.Join(qualifications, ", ") + ")"
	}
	return s
}

type namedFilterFlags struct {
	Dirty, Unpushed, Ahead, Behind bool
	Stashed, Empty, Local, Offline bool
}

func (o namedFilterFlags) signs() string {
	var b strings.Builder
	for _, item := range []struct {
		on   bool
		sign byte
	}{
		{o.Dirty, 'D'},
		{o.Unpushed, 'U'},
		{o.Ahead, 'A'},
		{o.Behind, 'B'},
		{o.Stashed, 'S'},
		{o.Empty, 'E'},
		{o.Local, 'L'},
		{o.Offline, 'O'},
	} {
		if item.on {
			b.WriteByte(item.sign)
		}
	}
	return b.String()
}

func selectedStates(only comb.SignSet) string {
	var states []string
	for _, item := range []struct {
		sign byte
		name string
	}{
		{'D', "uncommitted changes"},
		{'U', "unpushed commits"},
		{'A', "branches ahead of upstream"},
		{'B', "branches behind upstream"},
		{'S', "stashes"},
		{'E', "empty repositories"},
		{'L', "repositories without remotes"},
		{'O', "unreachable remotes"},
	} {
		if only.Has(item.sign) {
			states = append(states, item.name)
		}
	}
	if len(states) == 0 {
		return "none"
	}
	return joinNatural(states)
}

func joinNatural(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", and " + items[len(items)-1]
	}
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

// outputWidth follows diffstat: use terminal width when available and
// 80 columns for redirected output or when the terminal cannot report it.
func outputWidth(out io.Writer) int {
	const fallback = 80
	f, ok := out.(*os.File)
	if !ok {
		return fallback
	}
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return fallback
	}
	if width, ok := terminalColumns(f); ok {
		return width
	}
	if width, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && width >= 40 {
		return width
	}
	return fallback
}
