package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jkbstf/git-comb/internal/comb"
)

const (
	progressDelay    = 300 * time.Millisecond
	progressInterval = 250 * time.Millisecond
	slowDetailDelay  = time.Second
)

type activeProgressOperation struct {
	name    string
	started time.Time
}

// terminalProgress owns a transient status area on stderr. All terminal
// writes share mu with Suspend so streamed stdout blocks cannot collide with
// an in-flight repaint when both descriptors point at the same terminal.
type terminalProgress struct {
	mu         sync.Mutex
	w          io.Writer
	ansi       bool
	started    time.Time
	width      int
	phase      string
	entries    int
	dirs       int
	found      int
	total      int
	checked    int
	attention  int
	failed     int
	active     int
	current    string
	currentAt  time.Time
	operations map[string]activeProgressOperation
	roots      []string
	frame      int
	lines      int
	lastWidth  int
	suspended  bool
	fetches    int
	stop       chan struct{}
	done       chan struct{}
	stopOnce   sync.Once
}

func newTerminalProgress(w io.Writer, roots []string) *terminalProgress {
	file, ok := w.(*os.File)
	if !ok || os.Getenv("TERM") == "dumb" {
		return nil
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return nil
	}
	p := &terminalProgress{
		w: w, ansi: terminalANSI(file), started: time.Now(), width: outputWidth(w),
		operations: make(map[string]activeProgressOperation),
		roots:      progressRoots(roots), stop: make(chan struct{}), done: make(chan struct{}),
	}
	go p.run()
	return p
}

func (p *terminalProgress) run() {
	ticker := time.NewTicker(progressInterval)
	defer ticker.Stop()
	defer close(p.done)
	for {
		select {
		case now := <-ticker.C:
			p.mu.Lock()
			if !p.suspended && p.fetches == 0 && now.Sub(p.started) >= progressDelay && p.phase != "" && p.phase != "complete" {
				p.drawLocked(now)
			}
			p.mu.Unlock()
		case <-p.stop:
			p.mu.Lock()
			p.clearLocked()
			p.mu.Unlock()
			return
		}
	}
}

func (p *terminalProgress) Update(event comb.ProgressEvent) {
	if p == nil {
		return
	}
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	switch event.Kind {
	case comb.ProgressPhase:
		p.phase = event.Phase
		if event.Total > 0 {
			p.total = event.Total
		}
	case comb.ProgressDiscovery:
		p.entries, p.dirs, p.found = event.Entries, event.Directories, event.Repositories
		if event.Path != p.current {
			p.current, p.currentAt = event.Path, now
		}
	case comb.ProgressRepositoryStart:
		p.active++
		p.total = event.Total
	case comb.ProgressRepositoryEnd:
		if p.active > 0 {
			p.active--
		}
		p.checked++
		if event.Attention {
			p.attention++
		}
		if event.Failed {
			p.failed++
		}
	case comb.ProgressGitStart:
		p.operations[event.Path] = activeProgressOperation{name: event.Operation, started: now}
		if event.Operation == "fetch" {
			p.fetches++
			p.clearLocked()
		}
	case comb.ProgressGitEnd:
		if operation, ok := p.operations[event.Path]; ok && operation.name == event.Operation {
			delete(p.operations, event.Path)
		}
		if event.Operation == "fetch" && p.fetches > 0 {
			p.fetches--
		}
	}
}

// Suspend clears progress while fn writes durable output, then redraws it if
// work remains. It is also used around output that may interact with a tty.
func (p *terminalProgress) Suspend(fn func()) {
	if p == nil {
		fn()
		return
	}
	p.mu.Lock()
	p.suspended = true
	p.clearLocked()
	p.mu.Unlock()
	fn()
	p.mu.Lock()
	p.suspended = false
	if p.fetches == 0 && time.Since(p.started) >= progressDelay && p.phase != "" && p.phase != "complete" {
		p.drawLocked(time.Now())
	}
	p.mu.Unlock()
}

func (p *terminalProgress) Stop() {
	if p == nil {
		return
	}
	p.stopOnce.Do(func() {
		close(p.stop)
		<-p.done
	})
}

func (p *terminalProgress) drawLocked(now time.Time) {
	p.frame++
	frames := "|/-\\"
	spinner := frames[p.frame%len(frames)]
	elapsed := formatProgressDuration(now.Sub(p.started))
	var line string
	switch p.phase {
	case "scanning":
		line = fmt.Sprintf("[%c] Scanning  %s entries | %s dirs | %d found", spinner, groupedDecimal(p.entries), groupedDecimal(p.dirs), p.found)
		if p.checked > 0 {
			line += fmt.Sprintf(" | %d checked", p.checked)
		}
	case "preparing":
		line = fmt.Sprintf("[%c] Preparing %d repositories", spinner, p.total)
	default:
		line = fmt.Sprintf("[%c] Checking  %d/%d | %d active | %d need attention", spinner, p.checked, p.total, p.active, p.attention)
		if p.failed > 0 {
			line += fmt.Sprintf(" | %d failed", p.failed)
		}
	}
	line += " | " + elapsed
	line = trimProgressLine(line, max(1, p.width-1))

	detail := p.slowDetail(now)
	if p.ansi {
		p.drawANSILocked(line, detail)
		return
	}

	// Write the replacement and any padding together. Some terminals expose
	// themselves as Windows character devices without ANSI support; clearing
	// in one write and drawing in another gives those terminals a visible blank
	// frame between updates.
	lineWidth := utf8.RuneCountInString(line)
	padding := max(0, p.lastWidth-lineWidth)
	_, _ = io.WriteString(p.w, "\r"+line+strings.Repeat(" ", padding))
	p.lastWidth = lineWidth
	p.lines = 1
}

func (p *terminalProgress) drawANSILocked(line, detail string) {
	oldLines := p.lines
	twoLines := p.width >= 100 && detail != ""
	var output strings.Builder
	if oldLines == 2 {
		output.WriteString("\r\x1b[1A")
	} else {
		output.WriteByte('\r')
	}
	output.WriteString(line)
	output.WriteString("\x1b[K")
	if twoLines {
		output.WriteString("\n\r")
		output.WriteString(trimProgressLine("    "+detail, max(1, p.width-1)))
		output.WriteString("\x1b[K")
		p.lines = 2
	} else {
		if oldLines == 2 {
			output.WriteString("\n\r\x1b[2K\x1b[1A")
		}
		p.lines = 1
	}
	_, _ = io.WriteString(p.w, output.String())
}

func (p *terminalProgress) slowDetail(now time.Time) string {
	var slowPath string
	var slow activeProgressOperation
	for path, operation := range p.operations {
		if slowPath == "" || operation.started.Before(slow.started) {
			slowPath, slow = path, operation
		}
	}
	if slowPath != "" && now.Sub(slow.started) >= slowDetailDelay {
		return fmt.Sprintf("slowest: %s | %s | %s", p.path(slowPath), slow.name, formatProgressDuration(now.Sub(slow.started)))
	}
	if p.phase == "scanning" && p.current != "" && now.Sub(p.currentAt) >= slowDetailDelay {
		return "in " + p.path(p.current)
	}
	return ""
}

func (p *terminalProgress) clearLocked() {
	if !p.ansi {
		if p.lines > 0 {
			fmt.Fprintf(p.w, "\r%s\r", strings.Repeat(" ", p.lastWidth))
		}
		p.lines, p.lastWidth = 0, 0
		return
	}
	switch p.lines {
	case 1:
		fmt.Fprint(p.w, "\r\x1b[2K")
	case 2:
		fmt.Fprint(p.w, "\r\x1b[2K\x1b[1A\r\x1b[2K")
	}
	p.lines = 0
}

func progressRoots(roots []string) []string {
	resolved := make([]string, 0, len(roots))
	for _, root := range roots {
		path, err := filepath.Abs(root)
		if err == nil {
			if evaluated, evalErr := filepath.EvalSymlinks(path); evalErr == nil {
				path = evaluated
			}
			resolved = append(resolved, filepath.Clean(path))
		}
	}
	return resolved
}

func (p *terminalProgress) path(path string) string {
	best := path
	bestDepth := int(^uint(0) >> 1)
	for _, root := range p.roots {
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		depth := strings.Count(rel, string(filepath.Separator))
		if depth < bestDepth {
			best, bestDepth = rel, depth
		}
	}
	return best
}

func groupedDecimal(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func formatProgressDuration(duration time.Duration) string {
	if duration < 10*time.Second {
		return fmt.Sprintf("%.1fs", duration.Seconds())
	}
	return strconv.FormatInt(int64(duration.Round(time.Second)/time.Second), 10) + "s"
}

func trimProgressLine(line string, width int) string {
	if width <= 0 || utf8.RuneCountInString(line) <= width {
		return line
	}
	runes := []rune(line)
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}
