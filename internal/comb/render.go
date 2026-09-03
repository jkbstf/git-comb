package comb

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// RenderOptions configure Render.
type RenderOptions struct {
	// Roots are the scan targets. Repository paths are rendered
	// relative to the nearest matching root; when empty, Report.Path
	// is rendered unchanged.
	Roots []string
	// All keeps clean repositories in the output.
	All bool
	// Short selects the compact, one-line-per-repository sign view.
	Short bool
	// Color highlights findings and branches.
	Color bool
	// Width is the available output width in terminal columns. Zero
	// uses Git's non-terminal diffstat fallback of 80 columns.
	Width int
	// Only restricts rendering and attention counting to selected
	// finding classes; probe failures are always rendered.
	Only SignSet
}

const (
	// signColumn fits compact sign combinations with some breathing room.
	signColumn = 6
	// defaultGroupedWidth matches Git's diffstat fallback when terminal
	// width is unavailable.
	defaultGroupedWidth = 80
	columnGap           = "  "
)

const (
	ansiRed   = "\x1b[0;31m"
	ansiGreen = "\x1b[0;32m"
	ansiReset = "\x1b[0m"
)

type renderColors struct {
	red, green, reset string
}

type labelSegment struct {
	text             string
	color, reset     string
	trimmable        bool
	minimumTrimWidth int
}

type tableRow struct {
	subject []labelSegment
	context []labelSegment
	detail  string
}

type tableLayout struct {
	subject, context      int
	hasContext, hasDetail bool
}

func colors(enabled bool) renderColors {
	if !enabled {
		return renderColors{}
	}
	return renderColors{red: ansiRed, green: ansiGreen, reset: ansiReset}
}

// Render writes the selected view and returns the number of unique
// repositories needing attention and the number that could not be
// probed. The default view groups repositories by state; Short keeps
// the compact sign view used before v0.4.
func Render(w io.Writer, reports []Report, opts RenderOptions) (attention, failed int) {
	attention, failed = countResults(reports, opts.Only)
	paths := newDisplayPaths(opts.Roots)
	if opts.Short {
		renderShort(w, reports, opts, paths)
	} else {
		renderGrouped(w, reports, opts, paths)
	}
	return attention, failed
}

func countResults(reports []Report, only SignSet) (attention, failed int) {
	for _, r := range reports {
		if r.Ignored {
			continue
		}
		if r.Err != nil {
			failed++
			continue
		}
		if only.Filter(r.Signs()) != "" {
			attention++
		}
	}
	return attention, failed
}

// renderShort is deliberately stable: scripts and people that prefer
// the sign vocabulary can request the former default with -s.
func renderShort(w io.Writer, reports []Report, opts RenderOptions, paths displayPaths) {
	c := colors(opts.Color)
	for _, r := range reports {
		if r.Ignored {
			continue
		}
		path := paths.forRepo(r.Path)
		if r.Err != nil {
			fmt.Fprintf(w, "%s%-*s%s %s: %v\n", c.red, signColumn, "!", c.reset, path, r.Err)
			continue
		}
		signs := opts.Only.Filter(r.Signs())
		if signs == "" {
			if opts.All {
				fmt.Fprintf(w, "%-*s %s\n", signColumn, "", path)
			}
			continue
		}
		fmt.Fprintf(w, "%s%-*s%s %s\n", c.red, signColumn, signs, c.reset, path)
	}
}

type findingGroup struct {
	sign  byte
	title string
	// showHead adds the current ref context for worktree-specific
	// findings. Repository-wide findings deliberately omit it.
	showHead bool
}

var findingGroups = []findingGroup{
	{sign: 'S', title: "Stashes:"},
	{sign: 'E', title: "Empty repositories:"},
	{sign: 'O', title: "Unreachable remotes:"},
}

func renderGrouped(w io.Writer, reports []Report, opts RenderOptions, paths displayPaths) {
	c := colors(opts.Color)
	width := normalizedRenderWidth(opts.Width)
	wroteSection := false

	if opts.Only.Has('D') {
		renderFindingGroup(w, reports, paths, c, width,
			findingGroup{sign: 'D', title: "Uncommitted changes:", showHead: true}, &wroteSection)
	}
	if opts.Only.Has('L') {
		renderFindingGroup(w, reports, paths, c, width,
			findingGroup{sign: 'L', title: "No remotes:"}, &wroteSection)
	}
	renderBranchSection(w, reports, opts, paths, c, width, &wroteSection)
	for _, group := range findingGroups {
		if opts.Only.Has(group.sign) {
			renderFindingGroup(w, reports, paths, c, width, group, &wroteSection)
		}
	}

	if opts.All {
		var clean []Report
		for _, r := range reports {
			if !r.Ignored && r.Err == nil && opts.Only.Filter(r.Signs()) == "" {
				clean = append(clean, r)
			}
		}
		if len(clean) > 0 {
			writeSectionStart(w, "Clean repositories:", &wroteSection)
			rows := make([]tableRow, 0, len(clean))
			for _, r := range clean {
				rows = append(rows, repoTableRow("  ", paths.forRepo(r.Path), r.Branch, "", c))
			}
			writeTable(w, rows, width)
		}
	}

	var broken []Report
	for _, r := range reports {
		if !r.Ignored && r.Err != nil {
			broken = append(broken, r)
		}
	}
	if len(broken) > 0 {
		writeSectionStart(w, "Inspection failures:", &wroteSection)
		for _, r := range broken {
			label, _ := fitLabel([]labelSegment{
				{text: "  "},
				{text: paths.forRepo(r.Path), color: c.red, reset: c.reset, trimmable: true, minimumTrimWidth: 12},
			}, width)
			fmt.Fprintf(w, "%s: %v\n", label, r.Err)
		}
	}
}

func renderFindingGroup(w io.Writer, reports []Report, paths displayPaths, c renderColors, width int, group findingGroup, wroteSection *bool) {
	members := reportsForSign(reports, group.sign)
	if len(members) == 0 {
		return
	}
	writeSectionStart(w, group.title, wroteSection)
	rows := make([]tableRow, 0, len(members))
	for _, r := range members {
		branch := ""
		if group.showHead {
			branch = r.Branch
		}
		row := repoTableRow("  ", paths.forRepo(r.Path), branch, c.red, c)
		row.detail = findingDetail(r, group.sign)
		rows = append(rows, row)
	}
	writeTable(w, rows, width)
}

func renderBranchSection(w io.Writer, reports []Report, opts RenderOptions, paths displayPaths, c renderColors, width int, wroteSection *bool) {
	if !opts.Only.Has('U') && !opts.Only.Has('A') && !opts.Only.Has('B') {
		return
	}
	type branchReport struct {
		report   Report
		branches []BranchStatus
	}
	var members []branchReport
	for _, r := range reports {
		if r.Ignored || r.Err != nil {
			continue
		}
		branches := selectedBranches(r.Branches, opts.Only)
		if len(branches) > 0 {
			members = append(members, branchReport{report: r, branches: branches})
		}
	}
	if len(members) == 0 {
		return
	}

	writeSectionStart(w, "Branches:", wroteSection)
	var allRows []tableRow
	for _, member := range members {
		for _, branch := range member.branches {
			allRows = append(allRows, branchTableRow(branch, member.report.Branch, c))
		}
	}
	layout := layoutTable(allRows, width)
	for _, member := range members {
		r := member.report
		label, _ := fitLabel([]labelSegment{
			{text: "  "},
			{text: paths.forRepo(r.Path), color: c.red, reset: c.reset, trimmable: true, minimumTrimWidth: 12},
		}, width)
		fmt.Fprintln(w, label)
		for _, branch := range member.branches {
			writeTableRow(w, branchTableRow(branch, r.Branch, c), layout)
		}
	}
}

func selectedBranches(branches []BranchStatus, only SignSet) []BranchStatus {
	selected := make([]BranchStatus, 0, len(branches))
	for _, branch := range branches {
		if !only.Has('U') {
			branch.Unpushed = 0
		}
		if !only.Has('A') {
			branch.Ahead = 0
		}
		if !only.Has('B') {
			branch.Behind = 0
		}
		if branch.Unpushed > 0 || branch.Ahead > 0 || branch.Behind > 0 {
			selected = append(selected, branch)
		}
	}
	slices.SortFunc(selected, func(a, b BranchStatus) int {
		if d := branchPriority(a) - branchPriority(b); d != 0 {
			return d
		}
		return strings.Compare(a.Name, b.Name)
	})
	return selected
}

func branchDetail(branch BranchStatus) string {
	var parts []string
	if branch.Unpushed > 0 {
		parts = append(parts, fmt.Sprintf("%d unpushed %s", branch.Unpushed, plural(branch.Unpushed, "commit", "commits")))
	} else if branch.Ahead > 0 {
		// With no local-only commits, ahead means the branch differs from
		// its upstream but its commits already exist through some remote
		// ref. Keep that useful fact without repeating ahead beside
		// unpushed in the more common case above.
		parts = append(parts, fmt.Sprintf("%d %s ahead", branch.Ahead, plural(branch.Ahead, "commit", "commits")))
	}
	if branch.Behind > 0 {
		parts = append(parts, fmt.Sprintf("%d %s behind", branch.Behind, plural(branch.Behind, "commit", "commits")))
	}
	if branch.UpstreamGone {
		parts = append(parts, "upstream gone")
	} else if branch.Upstream == "" && !branch.Detached {
		parts = append(parts, "no upstream")
	}
	return strings.Join(parts, ", ")
}

func branchTableRow(branch BranchStatus, current string, c renderColors) tableRow {
	marker := "  "
	branchColor, branchReset := "", ""
	if branch.Detached {
		marker = "* "
	} else if branch.Name == current {
		marker = "* "
		branchColor, branchReset = c.green, c.reset
	} else if branch.InWorktree {
		marker = "+ "
	}

	subject := []labelSegment{
		{text: "    " + marker},
		{text: branch.Name, color: branchColor, reset: branchReset, trimmable: true, minimumTrimWidth: 12},
	}
	var context []labelSegment
	if branch.Upstream != "" {
		context = []labelSegment{
			{text: "["},
			{text: branch.Upstream, trimmable: true, minimumTrimWidth: 12},
			{text: "]"},
		}
	}
	return tableRow{subject: subject, context: context, detail: branchDetail(branch)}
}

func repoTableRow(indent, path, branch, pathColor string, c renderColors) tableRow {
	row := tableRow{subject: []labelSegment{
		{text: indent},
		{text: path, color: pathColor, reset: c.reset, trimmable: true, minimumTrimWidth: 12},
	}}
	if branch != "" {
		row.context = []labelSegment{
			{text: "["},
			{text: branch, color: c.green, reset: c.reset, trimmable: true, minimumTrimWidth: 8},
			{text: "]"},
		}
	}
	return row
}

func normalizedRenderWidth(width int) int {
	if width <= 0 {
		return defaultGroupedWidth
	}
	if width < 40 {
		return 40
	}
	return width
}

func writeTable(w io.Writer, rows []tableRow, width int) {
	layout := layoutTable(rows, width)
	for _, row := range rows {
		writeTableRow(w, row, layout)
	}
}

func layoutTable(rows []tableRow, width int) tableLayout {
	maxSubject, minSubject := 0, 0
	maxContext, minContext := 0, 0
	maxDetail := 0
	layout := tableLayout{}
	for _, row := range rows {
		maxSubject = max(maxSubject, labelWidth(row.subject))
		minSubject = max(minSubject, labelMinimumWidth(row.subject))
		if len(row.context) > 0 {
			layout.hasContext = true
			maxContext = max(maxContext, labelWidth(row.context))
			minContext = max(minContext, labelMinimumWidth(row.context))
		}
		if row.detail != "" {
			layout.hasDetail = true
			maxDetail = max(maxDetail, utf8.RuneCountInString(row.detail))
		}
	}

	gaps := 0
	if layout.hasContext {
		gaps += utf8.RuneCountInString(columnGap)
	}
	if layout.hasDetail {
		gaps += utf8.RuneCountInString(columnGap)
	}
	available := width - gaps
	if layout.hasDetail {
		available -= maxDetail
	}

	subjectCap, contextCap := maxSubject, maxContext
	if layout.hasContext {
		for subjectCap+contextCap > available {
			subjectSpare := subjectCap - minSubject
			contextSpare := contextCap - minContext
			if subjectSpare <= 0 && contextSpare <= 0 {
				break
			}
			if subjectSpare >= contextSpare && subjectSpare > 0 {
				subjectCap--
			} else {
				contextCap--
			}
		}
	} else if subjectCap > available {
		subjectCap = max(1, available)
	}

	for _, row := range rows {
		_, visible := fitLabel(row.subject, subjectCap)
		layout.subject = max(layout.subject, visible)
		if layout.hasContext {
			_, visible = fitLabel(row.context, contextCap)
			layout.context = max(layout.context, visible)
		}
	}
	return layout
}

func writeTableRow(w io.Writer, row tableRow, layout tableLayout) {
	subject, subjectWidth := fitLabel(row.subject, layout.subject)
	var line strings.Builder
	line.WriteString(subject)

	if !layout.hasContext {
		if row.detail == "" {
			fmt.Fprintln(w, line.String())
			return
		}
		appendColumnGap(&line, layout.subject-subjectWidth)
		line.WriteString(row.detail)
		fmt.Fprintln(w, line.String())
		return
	}

	if len(row.context) == 0 && row.detail == "" {
		fmt.Fprintln(w, line.String())
		return
	}
	appendColumnGap(&line, layout.subject-subjectWidth)
	context, contextWidth := fitLabel(row.context, layout.context)
	line.WriteString(context)
	if row.detail == "" {
		fmt.Fprintln(w, line.String())
		return
	}
	appendColumnGap(&line, layout.context-contextWidth)
	line.WriteString(row.detail)
	fmt.Fprintln(w, line.String())
}

func appendColumnGap(line *strings.Builder, padding int) {
	line.WriteString(strings.Repeat(" ", padding))
	line.WriteString(columnGap)
}

func labelWidth(segments []labelSegment) int {
	width := 0
	for _, segment := range segments {
		width += utf8.RuneCountInString(segment.text)
	}
	return width
}

func labelMinimumWidth(segments []labelSegment) int {
	width := 0
	for _, segment := range segments {
		segmentWidth := utf8.RuneCountInString(segment.text)
		if segment.trimmable {
			segmentWidth = min(segmentWidth, segment.minimumTrimWidth)
		}
		width += segmentWidth
	}
	return width
}

func fitLabel(segments []labelSegment, maxWidth int) (string, int) {
	widths := make([]int, len(segments))
	total := 0
	for i, segment := range segments {
		widths[i] = utf8.RuneCountInString(segment.text)
		total += widths[i]
	}
	for total > maxWidth {
		best, spare := -1, 0
		for i, segment := range segments {
			if !segment.trimmable {
				continue
			}
			minimum := min(widths[i], segment.minimumTrimWidth)
			if widths[i]-minimum > spare {
				best, spare = i, widths[i]-minimum
			}
		}
		if best < 0 {
			break
		}
		widths[best]--
		total--
	}

	var b strings.Builder
	for i, segment := range segments {
		text := middleTrim(segment.text, widths[i])
		if segment.color != "" {
			b.WriteString(segment.color)
		}
		b.WriteString(text)
		if segment.color != "" {
			b.WriteString(segment.reset)
		}
	}
	return b.String(), total
}

func middleTrim(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width <= 0 {
		return ""
	}
	if width <= 3 {
		return string(runes[:width])
	}
	remaining := width - 3
	left := (remaining + 1) / 2
	right := remaining - left
	return string(runes[:left]) + "..." + string(runes[len(runes)-right:])
}

func reportsForSign(reports []Report, sign byte) []Report {
	var matching []Report
	for _, r := range reports {
		if r.Ignored || r.Err != nil {
			continue
		}
		if strings.IndexByte(r.Signs(), sign) >= 0 {
			matching = append(matching, r)
		}
	}
	return matching
}

func findingDetail(r Report, sign byte) string {
	switch sign {
	case 'D':
		if summary := formatShortStat(r.DirtyStat); summary != "" {
			return summary
		}
	case 'S':
		return fmt.Sprintf("%d %s", r.Stashes, plural(r.Stashes, "stash", "stashes"))
	}
	return ""
}

func formatShortStat(stat ShortStat) string {
	var parts []string
	if stat.FilesChanged > 0 {
		changed := fmt.Sprintf("%d %s changed", stat.FilesChanged, plural(stat.FilesChanged, "file", "files"))
		if stat.Insertions > 0 || stat.Deletions > 0 {
			changed += fmt.Sprintf(": +%d/-%d", stat.Insertions, stat.Deletions)
		}
		parts = append(parts, changed)
	}
	if stat.Untracked > 0 {
		parts = append(parts, fmt.Sprintf("%d untracked %s", stat.Untracked, plural(stat.Untracked, "file", "files")))
	}
	return strings.Join(parts, ", ")
}

type displayPaths []string

func newDisplayPaths(roots []string) displayPaths {
	var resolved displayPaths
	for _, root := range roots {
		path, err := resolveRoot(root)
		if err == nil {
			resolved = append(resolved, path)
		}
	}
	return resolved
}

// forRepo returns the repository path relative to the most specific
// scan root that contains it. Report.Path remains absolute so probing,
// grouping, and deduplication never depend on presentation.
func (roots displayPaths) forRepo(repo string) string {
	if len(roots) == 0 {
		return repo
	}
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		repo = resolved
	}
	best, bestDepth := "", int(^uint(0)>>1)
	for _, root := range roots {
		rel, err := filepath.Rel(root, repo)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		depth := 0
		if rel != "." {
			depth = strings.Count(rel, string(filepath.Separator)) + 1
		}
		if depth < bestDepth {
			best, bestDepth = rel, depth
		}
	}
	if best != "" {
		return best
	}
	return repo
}

func writeSectionStart(w io.Writer, title string, wroteSection *bool) {
	if *wroteSection {
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, title)
	*wroteSection = true
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
