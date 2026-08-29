package comb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSettings(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	dir := tempDir(t)

	s, err := LoadSettings(dir)
	if err != nil {
		t.Fatalf("LoadSettings on empty config: %v", err)
	}
	if len(s.Prune) != 0 || s.Jobs != 0 || s.Hidden {
		t.Errorf("empty config produced settings: %+v", s)
	}

	cfg := os.Getenv("GIT_CONFIG_GLOBAL")
	content := "[comb]\n\tprune = _deps\n\tprune = build\n\tjobs = 12\n\thidden = yes\n"
	if err := os.WriteFile(cfg, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err = LoadSettings(dir)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if len(s.Prune) != 2 || s.Prune[0] != "_deps" || s.Prune[1] != "build" {
		t.Errorf("Prune = %v, want [_deps build]", s.Prune)
	}
	if s.Jobs != 12 || !s.Hidden {
		t.Errorf("Jobs, Hidden = %d, %v; want 12, true", s.Jobs, s.Hidden)
	}

	if err := os.WriteFile(cfg, []byte("[comb]\n\tjobs = many\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSettings(dir); err == nil {
		t.Error("invalid comb.jobs accepted")
	}
}

func TestRepoIgnoresReadsBothKeys(t *testing.T) {
	requireGit(t)
	setupGitEnv(t)
	repo := filepath.Join(tempDir(t), "repo")
	initRepo(t, repo)
	mustGit(t, repo, "config", "comb.ignore", "true")
	mustGit(t, repo, "config", "--add", "comb.ignoreBranch", "backup/*")
	mustGit(t, repo, "config", "--add", "comb.ignoreBranch", "spike/*")

	ignored, globs, err := repoIgnores(repo)
	if err != nil {
		t.Fatalf("repoIgnores: %v", err)
	}
	if !ignored {
		t.Error("comb.ignore not read")
	}
	if len(globs) != 2 || globs[0] != "backup/*" || globs[1] != "spike/*" {
		t.Errorf("globs = %v (camelCase keys must match case-insensitively)", globs)
	}
}

func TestGitBool(t *testing.T) {
	for _, v := range []string{"true", "YES", "on", "1"} {
		if b, err := gitBool(v); err != nil || !b {
			t.Errorf("gitBool(%q) = %v, %v; want true", v, b, err)
		}
	}
	for _, v := range []string{"false", "No", "off", "0", ""} {
		if b, err := gitBool(v); err != nil || b {
			t.Errorf("gitBool(%q) = %v, %v; want false", v, b, err)
		}
	}
	if _, err := gitBool("maybe"); err == nil {
		t.Error("gitBool accepted nonsense")
	}
}

func TestPartitionBranches(t *testing.T) {
	kept, acked, err := partitionBranches(
		[]string{"master", "backup/a", "backup/a/b", "spike/x"},
		[]string{"backup/*", "spike/*"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 2 || kept[0] != "master" || kept[1] != "backup/a/b" {
		t.Errorf("kept = %v; * must not cross /", kept)
	}
	if len(acked) != 2 {
		t.Errorf("acked = %v", acked)
	}

	if _, _, err := partitionBranches([]string{"x"}, []string{"["}); err == nil {
		t.Error("bad pattern accepted")
	}
}
