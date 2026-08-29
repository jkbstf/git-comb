package comb

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// gitOut runs git in repo and returns its standard output. Every
// invocation passes --no-optional-locks so a probe can never take a
// lock an editor or IDE is waiting on.
func gitOut(repo string, args ...string) (string, error) {
	argv := append([]string{"-C", repo, "--no-optional-locks"}, args...)
	cmd := exec.Command("git", argv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], firstLine(msg))
	}
	return stdout.String(), nil
}

// gitCount runs a git command whose entire output is one integer.
func gitCount(repo string, args ...string) (int, error) {
	out, err := gitOut(repo, args...)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
