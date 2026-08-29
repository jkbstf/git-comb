package comb

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// repoLocationEnv lists the environment variables through which git
// resolves a repository somewhere other than -C says. Inherited from a
// hook or a script, any one of them would silently point every probe
// at the same repository, so they are scrubbed from child processes.
var repoLocationEnv = []string{
	"GIT_DIR",
	"GIT_WORK_TREE",
	"GIT_INDEX_FILE",
	"GIT_COMMON_DIR",
	"GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_NAMESPACE",
	"GIT_PREFIX",
}

// gitEnv is the child environment: the parent's, minus the
// repo-location overrides, plus any extras.
func gitEnv(extra ...string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, kv := range os.Environ() {
		name, _, ok := strings.Cut(kv, "=")
		if ok && isRepoLocationVar(name) {
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

func isRepoLocationVar(name string) bool {
	for _, v := range repoLocationEnv {
		if name == v {
			return true
		}
	}
	return false
}

// gitOut runs git in repo and returns its standard output. Every
// invocation passes --no-optional-locks so a probe can never take a
// lock an editor or IDE is waiting on.
func gitOut(repo string, args ...string) (string, error) {
	return gitOutEnv(repo, nil, args...)
}

// gitOutEnv is gitOut with extra environment variables for the child.
func gitOutEnv(repo string, extraEnv []string, args ...string) (string, error) {
	argv := append([]string{"-C", repo, "--no-optional-locks"}, args...)
	cmd := exec.Command("git", argv...)
	cmd.Env = gitEnv(extraEnv...)
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
