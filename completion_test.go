package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompletionScriptsCoverEveryLongOption(t *testing.T) {
	paths := []string{
		"contrib/completion/git-comb-completion.bash",
		"contrib/completion/git-comb-completion.zsh",
		"contrib/completion/git-comb.fish",
	}
	options := []string{
		"short", "all", "only-dirty", "only-unpushed", "only-ahead",
		"only-behind", "only-stashed", "only-empty", "only-local",
		"only-offline", "exclude-dirty", "exclude-unpushed", "exclude-ahead",
		"exclude-behind", "exclude-stashed", "exclude-empty", "exclude-local",
		"exclude-offline", "only", "except", "jobs", "fetch", "hidden", "prune",
		"no-ignores", "color", "diagnostics", "version", "help",
	}
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, option := range options {
			if !strings.Contains(string(content), option) {
				t.Errorf("%s does not complete --%s", path, option)
			}
		}
		for _, value := range []string{"D", "U", "A", "B", "S", "E", "L", "O", "auto", "always", "never"} {
			if !strings.Contains(string(content), value) {
				t.Errorf("%s does not contain value %q", path, value)
			}
		}
	}
}

func TestCompletionScriptSyntax(t *testing.T) {
	tests := []struct {
		shell string
		path  string
	}{
		{"bash", "contrib/completion/git-comb-completion.bash"},
		{"zsh", "contrib/completion/git-comb-completion.zsh"},
		{"fish", "contrib/completion/git-comb.fish"},
	}
	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			shell, err := exec.LookPath(tt.shell)
			if err != nil {
				t.Skipf("%s is not on PATH", tt.shell)
			}
			args := []string{"-n", tt.path}
			if tt.shell == "fish" {
				args = []string{"--no-execute", tt.path}
			}
			if out, err := exec.Command(shell, args...).CombinedOutput(); err != nil {
				t.Fatalf("%s syntax: %v\n%s", tt.path, err, out)
			}
		})
	}
}

func TestBashCompletionBehavior(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash completion behavior is covered on Unix runners")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	path, err := filepath.Abs("contrib/completion/git-comb-completion.bash")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		words  string
		cword  string
		want   []string
		reject []string
	}{
		{
			name:   "descriptive options",
			words:  "git-comb --only-u",
			cword:  "1",
			want:   []string{"--only-unpushed"},
			reject: []string{"-o"},
		},
		{
			name:   "descriptive exclusions",
			words:  "git-comb --exclude-u",
			cword:  "1",
			want:   []string{"--exclude-unpushed"},
			reject: []string{"-x"},
		},
		{
			name:  "color values",
			words: "git-comb --color a",
			cword: "2",
			want:  []string{"auto", "always"},
		},
		{
			name:   "remaining signs",
			words:  "git-comb --only DU",
			cword:  "2",
			want:   []string{"DUA", "DUB", "DUS", "DUE", "DUL", "DUO"},
			reject: []string{"DUD", "DUU"},
		},
		{
			name:  "diagnostic file",
			words: "git-comb --diagnostics REA",
			cword: "2",
			want:  []string{"README.md"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := "source \"$1\"\nCOMP_WORDS=(" + tt.words + ")\nCOMP_CWORD=" + tt.cword + "\n__git_comb_wrap\nprintf '%s\\n' \"${COMPREPLY[@]}\"\n"
			out, err := exec.Command(bash, "-c", script, "bash", path).CombinedOutput()
			if err != nil {
				t.Fatalf("completion failed: %v\n%s", err, out)
			}
			got := string(out)
			for _, want := range tt.want {
				if !containsLine(got, want) {
					t.Errorf("completion missing %q:\n%s", want, got)
				}
			}
			for _, reject := range tt.reject {
				if containsLine(got, reject) {
					t.Errorf("completion unexpectedly contains %q:\n%s", reject, got)
				}
			}
		})
	}
}

func TestBashCompletionProvidesGitExtensionFunction(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash completion behavior is covered on Unix runners")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	path, err := filepath.Abs("contrib/completion/git-comb-completion.bash")
	if err != nil {
		t.Fatal(err)
	}
	script := "source \"$1\"\ncur=--only-u\nprev=comb\nwords=(git comb --only-u)\ncword=2\n_git_comb\nprintf '%s\\n' \"${COMPREPLY[@]}\"\n"
	out, err := exec.Command(bash, "-c", script, "bash", path).CombinedOutput()
	if err != nil {
		t.Fatalf("Git-style completion failed: %v\n%s", err, out)
	}
	if !containsLine(string(out), "--only-unpushed") {
		t.Errorf("Git-style completion missing --only-unpushed:\n%s", out)
	}
}

func TestBashCompletionThroughGitDispatcher(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash completion behavior is covered on Unix runners")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	var gitCompletion string
	for _, candidate := range []string{
		"/opt/homebrew/etc/bash_completion.d/git-completion.bash",
		"/usr/local/etc/bash_completion.d/git-completion.bash",
		"/usr/share/bash-completion/completions/git",
		"/etc/bash_completion.d/git",
	} {
		if _, err := os.Stat(candidate); err == nil {
			gitCompletion = candidate
			break
		}
	}
	if gitCompletion == "" {
		t.Skip("Git's Bash completion script was not found")
	}
	path, err := filepath.Abs("contrib/completion/git-comb-completion.bash")
	if err != nil {
		t.Fatal(err)
	}
	script := "source \"$1\"\nsource \"$2\"\nCOMP_WORDS=(git comb --on)\nCOMP_CWORD=2\nspec=$(complete -p git)\nfunc=${spec#* -F }\nfunc=${func%% *}\n\"$func\"\nprintf '%s\\n' \"${COMPREPLY[@]}\"\n"
	out, err := exec.Command(bash, "-c", script, "bash", gitCompletion, path).CombinedOutput()
	if err != nil {
		t.Fatalf("Git dispatcher completion failed: %v\n%s", err, out)
	}
	for _, want := range []string{"--only", "--only-dirty", "--only-unpushed"} {
		if !containsLine(string(out), want) {
			t.Errorf("Git dispatcher completion missing %q:\n%s", want, out)
		}
	}
}

func TestZshCompletionBuildsRemainingSigns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Zsh completion behavior is covered on Unix runners")
	}
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not on PATH")
	}
	path, err := filepath.Abs("contrib/completion/git-comb-completion.zsh")
	if err != nil {
		t.Fatal(err)
	}
	script := "source \"$1\"\nfunction compadd { print -l -- \"${choices[@]}\"; print -l -- \"${displays[@]}\"; }\nPREFIX=DU\n_git_comb_signs\n"
	out, err := exec.Command(zsh, "-fc", script, "zsh", path).CombinedOutput()
	if err != nil {
		t.Fatalf("Zsh sign completion failed: %v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{
		"DU",
		"DUA",
		"DUO",
		"DU   -- uncommitted changes or unpushed commits",
		"DUA  -- uncommitted changes, unpushed commits, or branches ahead of their upstream",
		"DUO  -- uncommitted changes, unpushed commits, or unreachable remotes",
	} {
		if !containsLine(got, want) {
			t.Errorf("completion missing %q:\n%s", want, got)
		}
	}
	for _, reject := range []string{"DUD", "DUU"} {
		if containsLine(got, reject) {
			t.Errorf("completion unexpectedly contains %q:\n%s", reject, got)
		}
	}
}

func TestFishCompletionDescribesTheWholeSignSet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Fish completion behavior is covered on Unix runners")
	}
	fish, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish is not on PATH")
	}
	path, err := filepath.Abs("contrib/completion/git-comb.fish")
	if err != nil {
		t.Fatal(err)
	}
	script := "source \"$argv[1]\"; __git_comb_sign_description BEU"
	out, err := exec.Command(fish, "-c", script, path).CombinedOutput()
	if err != nil {
		t.Fatalf("Fish sign description failed: %v\n%s", err, out)
	}
	if got, want := strings.TrimSpace(string(out)), "branches behind their upstream, empty repositories, or unpushed commits"; got != want {
		t.Errorf("description = %q, want %q", got, want)
	}
}

func TestZshCompletionRepairsGitBundledContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Zsh completion behavior is covered on Unix runners")
	}
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not on PATH")
	}
	path, err := filepath.Abs("contrib/completion/git-comb-completion.zsh")
	if err != nil {
		t.Fatal(err)
	}
	script := "source \"$1\"\nfunction compadd { [[ ${words[CURRENT]-} = $cur ]] || exit 42; print context-ok; }\ncur=--on\nprev=comb\nwords=(git)\nCURRENT=2\nPREFIX=--on\n_git_comb\n"
	out, err := exec.Command(zsh, "-fc", script, "zsh", path).CombinedOutput()
	if err != nil {
		t.Fatalf("Zsh Git context repair failed: %v\n%s", err, out)
	}
	if !containsLine(string(out), "context-ok") {
		t.Errorf("Zsh Git context was not exercised:\n%s", out)
	}
}

func TestZshCompletionProvidesBothGitExtensionFunctions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Zsh completion behavior is covered on Unix runners")
	}
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not on PATH")
	}
	path, err := filepath.Abs("contrib/completion/git-comb-completion.zsh")
	if err != nil {
		t.Fatal(err)
	}
	script := "source \"$1\"\nfunctions _git_comb >/dev/null || exit 1\nfunctions _git-comb >/dev/null || exit 1\nfunction _git_comb_direct { print dispatched; }\n_git-comb\n"
	out, err := exec.Command(zsh, "-fc", script, "zsh", path).CombinedOutput()
	if err != nil {
		t.Fatalf("Zsh Git extension check failed: %v\n%s", err, out)
	}
	if !containsLine(string(out), "dispatched") {
		t.Errorf("hyphenated Git extension did not dispatch:\n%s", out)
	}
}

func containsLine(text, want string) bool {
	for _, line := range strings.Split(text, "\n") {
		if line == want {
			return true
		}
	}
	return false
}
