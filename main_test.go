package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// capture runs f with a pipe as the stream and returns what f wrote.
func capture(t *testing.T, f func(w *os.File)) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	done := make(chan string)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- b.String()
	}()
	f(w)
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	out := <-done
	if err := r.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}
	return out
}

func TestRunVersion(t *testing.T) {
	var code int
	out := capture(t, func(w *os.File) {
		code = run([]string{"--version"}, w, w)
	})
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.HasPrefix(out, "git-comb ") {
		t.Errorf("version output = %q", out)
	}
}

func TestRunHelp(t *testing.T) {
	var code int
	out := capture(t, func(w *os.File) {
		code = run([]string{"--help"}, w, w)
	})
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	for _, want := range []string{"Usage: git comb", "--fetch", "Exit status"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q", want)
		}
	}
}

func TestRunRejectsUnknownFlag(t *testing.T) {
	var code int
	out := capture(t, func(w *os.File) {
		code = run([]string{"--nonsense"}, w, w)
	})
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(out, "git-comb:") {
		t.Errorf("error output = %q", out)
	}
}

func TestRunRejectsBadColor(t *testing.T) {
	var code int
	out := capture(t, func(w *os.File) {
		code = run([]string{"--color", "sometimes"}, w, w)
	})
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(out, "invalid --color") {
		t.Errorf("error output = %q", out)
	}
}

func TestRunMissingRootExitsTwo(t *testing.T) {
	var code int
	capture(t, func(w *os.File) {
		code = run([]string{filepath.Join(t.TempDir(), "missing")}, w, w)
	})
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

func TestRunEmptyTreeExitsZero(t *testing.T) {
	var code int
	out := capture(t, func(w *os.File) {
		code = run([]string{t.TempDir()}, w, w)
	})
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "combed 0 repositories") {
		t.Errorf("summary missing: %q", out)
	}
}

func TestColorEnabled(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	if on, err := colorEnabled("always", os.Stdout); err != nil || !on {
		t.Errorf("always = %v, %v", on, err)
	}
	if on, err := colorEnabled("never", os.Stdout); err != nil || on {
		t.Errorf("never = %v, %v", on, err)
	}
	if _, err := colorEnabled("sometimes", os.Stdout); err == nil {
		t.Error("invalid value accepted")
	}
	t.Setenv("NO_COLOR", "1")
	if on, _ := colorEnabled("auto", os.Stdout); on {
		t.Error("auto ignored NO_COLOR")
	}
}
