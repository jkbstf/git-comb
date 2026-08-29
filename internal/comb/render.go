package comb

import (
	"fmt"
	"io"
)

// RenderOptions configure Render.
type RenderOptions struct {
	// All keeps clean repositories in the output.
	All bool
	// Verbose prints per-branch unpushed detail under each line.
	Verbose bool
	// Color paints signs red and branches green.
	Color bool
	// Only restricts rendering and attention counting to selected
	// finding classes; probe failures are always rendered.
	Only SignSet
}

// signColumn fits the widest realistic combination: D U A B S R.
const signColumn = 6

const (
	ansiRed   = "\x1b[0;31m"
	ansiGreen = "\x1b[0;32m"
	ansiReset = "\x1b[0m"
)

// Render writes one line per repository needing attention (every
// repository with All) and returns how many need attention and how
// many could not be probed.
func Render(w io.Writer, reports []Report, opts RenderOptions) (attention, failed int) {
	red, green, reset := "", "", ""
	if opts.Color {
		red, green, reset = ansiRed, ansiGreen, ansiReset
	}
	for _, r := range reports {
		if r.Ignored {
			continue
		}
		if r.Err != nil {
			failed++
			fmt.Fprintf(w, "%s%-*s%s %s: %v\n", red, signColumn, "!", reset, r.Path, r.Err)
			continue
		}
		signs := opts.Only.Filter(r.Signs())
		if signs == "" {
			if opts.All {
				fmt.Fprintf(w, "%-*s %s [%s%s%s]\n", signColumn, "", r.Path, green, r.Branch, reset)
			}
			continue
		}
		attention++
		fmt.Fprintf(w, "%s%-*s%s %s [%s%s%s]\n", red, signColumn, signs, reset, r.Path, green, r.Branch, reset)
		if opts.Verbose {
			for _, b := range r.UnpushedBranches {
				fmt.Fprintf(w, "%-*s unpushed: %s (%d)\n", signColumn, "", b.Name, b.Commits)
			}
		}
	}
	return attention, failed
}
