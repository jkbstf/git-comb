package comb

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
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
// repo-location overrides.
func gitEnv() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		name, _, ok := strings.Cut(kv, "=")
		if ok && (isRepoLocationVar(name) || name == "LC_ALL") {
			continue
		}
		env = append(env, kv)
	}
	// for-each-ref's documented tracking description contains words as
	// well as counts. Pin Git's machine-readable output to the stable C
	// locale so parsing does not depend on the user's language.
	env = append(env, "LC_ALL=C")
	return env
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
	return (gitRunner{}).out(repo, args[0], args...)
}

type gitRunner struct {
	diagnostics *Diagnostics
	progress    ProgressFunc
}

var cachedGitExecutable struct {
	sync.Once
	path string
}

// gitExecutable resolves git once per git-comb process. On Windows,
// repeating PATH and PATHEXT lookup for every short-lived child adds
// filesystem work to an already expensive process boundary.
func gitExecutable() string {
	cachedGitExecutable.Do(func() {
		path, err := exec.LookPath("git")
		if err == nil {
			cachedGitExecutable.path = path
		} else {
			// Preserve exec.Command's established error reporting when Git
			// is absent rather than introducing a separate startup path.
			cachedGitExecutable.path = "git"
		}
	})
	return cachedGitExecutable.path
}

func (g gitRunner) out(repo, operation string, args ...string) (string, error) {
	return g.output(repo, operation, nil, args...)
}

func (g gitRunner) outInput(repo, operation, input string, args ...string) (string, error) {
	return g.output(repo, operation, strings.NewReader(input), args...)
}

func (g gitRunner) output(repo, operation string, stdin io.Reader, args ...string) (string, error) {
	argv := append([]string{"-C", repo, "--no-optional-locks"}, args...)
	cmd := exec.Command(gitExecutable(), argv...)
	cmd.Env = gitEnv()
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	reportProgress(g.progress, ProgressEvent{Kind: ProgressGitStart, Path: repo, Operation: operation})
	span := g.diagnostics.startGit(repo, operation)
	err := cmd.Run()
	result, exitCode := "ok", 0
	if err != nil {
		result, exitCode = "start_error", -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result, exitCode = "exit", exitErr.ExitCode()
		}
	}
	span.end(result, exitCode)
	reportProgress(g.progress, ProgressEvent{Kind: ProgressGitEnd, Path: repo, Operation: operation})
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], firstLine(msg))
	}
	return stdout.String(), nil
}

func (g gitRunner) count(repo, operation string, args ...string) (int, error) {
	out, err := g.out(repo, operation, args...)
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
