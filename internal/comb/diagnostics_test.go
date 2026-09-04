package comb

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDiagnosticsConcurrentStreamIsValidAndAnonymous(t *testing.T) {
	const (
		secretPath = "/company/secret/client/repository"
		secretRef  = "secret/customer-branch"
	)
	var buf bytes.Buffer
	d, err := NewDiagnostics(&buf, "test-version")
	if err != nil {
		t.Fatal(err)
	}
	d.RegisterRepositories([]string{secretPath, secretPath + "-two"})
	d.Options(DiagnosticOptions{Roots: 1, Jobs: 8, Prunes: 2, Selection: "DU", Fetch: true})
	phase := time.Now()
	d.Phase(secretRef, phase, map[string]int{"repositories": 2, secretRef: 99})
	d.Wait(secretPath, secretRef, time.Now())

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			repo := secretPath
			if i%2 == 1 {
				repo += "-two"
			}
			queued := time.Now()
			started := d.RepositoryStart(repo, queued)
			operation := "status"
			result := "ok"
			if i == 0 {
				operation = secretRef
				result = secretRef
			}
			span := d.startGit(repo, operation)
			span.end(result, 0)
			d.RepositoryEnd(repo, started)
		}(i)
	}
	wg.Wait()
	if err := d.Finish(1); err != nil {
		t.Fatal(err)
	}

	text := buf.String()
	for _, secret := range []string{secretPath, secretRef} {
		if strings.Contains(text, secret) {
			t.Fatalf("diagnostics leaked %q:\n%s", secret, text)
		}
	}
	for _, forbidden := range []string{`"path"`, `"argv"`, `"output"`, `"error"`, `"environment"`, `"hostname"`, `"username"`, `"timestamp"`, `"pid"`, `"branch"`, `"remote"`, `"url"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("diagnostics contain forbidden field %s:\n%s", forbidden, text)
		}
	}

	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) != 133 {
		t.Fatalf("diagnostic lines = %d, want 133", len(lines))
	}
	for i, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("line %d is not JSON: %v\n%s", i+1, err, line)
		}
		assertDiagnosticSchema(t, i+1, event)
	}
	if !strings.Contains(lines[0], `"event":"run_start"`) || !strings.Contains(lines[len(lines)-1], `"event":"run_end"`) {
		t.Fatalf("stream boundaries missing:\nfirst: %s\nlast: %s", lines[0], lines[len(lines)-1])
	}
	if !strings.Contains(text, `"unknown"`) {
		t.Fatal("unreviewed labels were not replaced with the fixed unknown value")
	}
}

func assertDiagnosticSchema(t *testing.T, line int, event map[string]any) {
	t.Helper()
	common := []string{"event", "offset_ms"}
	allowed := map[string][]string{
		"run_start":        {"schema", "version", "go_version", "os", "arch", "cpus"},
		"options":          {"options"},
		"phase_end":        {"phase", "duration_ms", "counts"},
		"wait_end":         {"repository", "resource", "duration_ms"},
		"repository_start": {"repository", "queue_ms"},
		"repository_end":   {"repository", "duration_ms", "git_processes", "git_duration_ms"},
		"git_start":        {"invocation", "repository", "operation"},
		"git_end":          {"invocation", "repository", "operation", "duration_ms", "result", "exit_code"},
		"run_end":          {"duration_ms", "exit_code", "git_processes", "max_active_git", "max_active_probes", "operations"},
	}
	kind, ok := event["event"].(string)
	if !ok {
		t.Fatalf("diagnostic line %d has no string event", line)
	}
	keys, ok := allowed[kind]
	if !ok {
		t.Fatalf("diagnostic line %d has unknown event %q", line, kind)
	}
	permitted := make(map[string]bool, len(common)+len(keys))
	for _, key := range append(common, keys...) {
		permitted[key] = true
	}
	for key := range event {
		if !permitted[key] {
			t.Fatalf("diagnostic line %d event %q has unreviewed field %q", line, kind, key)
		}
	}

	if options, ok := event["options"].(map[string]any); ok {
		assertDiagnosticObjectKeys(t, line, "options", options, []string{
			"roots", "jobs", "prunes", "selection", "short", "all", "fetch", "hidden", "no_ignores",
		})
	}
	if counts, ok := event["counts"].(map[string]any); ok {
		assertDiagnosticObjectKeys(t, line, "counts", counts, []string{
			"repositories", "entries", "directories", "hidden_skipped", "pruned", "unreadable", "groups", "linked_worktrees",
		})
	}
	if operations, ok := event["operations"].([]any); ok {
		for _, raw := range operations {
			operation, ok := raw.(map[string]any)
			if !ok {
				t.Fatalf("diagnostic line %d has malformed operation", line)
			}
			assertDiagnosticObjectKeys(t, line, "operation", operation, []string{
				"name", "count", "total_ms", "median_ms", "p95_ms", "maximum_ms",
			})
		}
	}
}

func assertDiagnosticObjectKeys(t *testing.T, line int, object string, got map[string]any, allowed []string) {
	t.Helper()
	permitted := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		permitted[key] = true
	}
	for key := range got {
		if !permitted[key] {
			t.Fatalf("diagnostic line %d %s has unreviewed field %q", line, object, key)
		}
	}
}

func TestDiagnosticsLeavesGitStartWhenRunDoesNotFinish(t *testing.T) {
	var buf bytes.Buffer
	d, err := NewDiagnostics(&buf, "test-version")
	if err != nil {
		t.Fatal(err)
	}
	d.RegisterRepositories([]string{"/secret/repo"})
	d.startGit("/secret/repo", "status")
	text := buf.String()
	if !strings.Contains(text, `"event":"git_start"`) || !strings.Contains(text, `"operation":"status"`) {
		t.Fatalf("partial stream lacks active command:\n%s", text)
	}
	if strings.Contains(text, "/secret/repo") {
		t.Fatalf("partial stream leaked repository path:\n%s", text)
	}
}

func TestDiagnosticRepositoryIDsExtendPastThreeDigits(t *testing.T) {
	if got := threeDigits(7); got != "007" {
		t.Errorf("threeDigits(7) = %q", got)
	}
	if got := threeDigits(1234); got != "1234" {
		t.Errorf("threeDigits(1234) = %q", got)
	}
}
