package comb

import (
	"fmt"
	"strconv"
	"strings"
)

// Settings are the scan-level defaults read from git config at
// startup. Command-line flags override their matching keys; prune
// values merge, and only/except compose by subtraction after each is
// resolved.
type Settings struct {
	Prune  []string
	Jobs   int
	Hidden bool
	Only   string
	Except string
}

// LoadSettings reads comb.* scan defaults from the merged git config
// visible from dir — global and system config everywhere, plus the
// local config when dir sits inside a repository.
func LoadSettings(dir string) (Settings, error) {
	entries, err := configEntries(dir)
	if err != nil {
		return Settings{}, err
	}
	var s Settings
	for _, e := range entries {
		switch e.key {
		case "comb.prune":
			s.Prune = append(s.Prune, e.value)
		case "comb.jobs":
			n, err := strconv.Atoi(e.value)
			if err != nil {
				return Settings{}, fmt.Errorf("comb.jobs: invalid value %q", e.value)
			}
			s.Jobs = n
		case "comb.hidden":
			b, err := gitBool(e.value)
			if err != nil {
				return Settings{}, fmt.Errorf("comb.hidden: %w", err)
			}
			s.Hidden = b
		case "comb.only":
			s.Only = e.value
		case "comb.except":
			s.Except = e.value
		}
	}
	return s, nil
}

// repoIgnores reads a repository's acknowledgment config: comb.ignore
// silences the whole repository, comb.ignoreBranch globs acknowledge
// deliberately local-only branches. Both merge local over global, so
// a global glob applies everywhere and a local one to that clone.
func repoIgnores(repo string) (ignored bool, globs []string, err error) {
	entries, err := configEntries(repo)
	if err != nil {
		return false, nil, err
	}
	for _, e := range entries {
		switch e.key {
		case "comb.ignore":
			b, err := gitBool(e.value)
			if err != nil {
				return false, nil, fmt.Errorf("comb.ignore: %w", err)
			}
			ignored = b
		case "comb.ignorebranch": // git lowercases config key names
			globs = append(globs, e.value)
		}
	}
	return ignored, globs, nil
}

type configEntry struct {
	key   string
	value string
}

// configEntries lists every comb.* config entry visible from dir.
// git exits 1 when nothing matches; that simply means no settings.
func configEntries(dir string) ([]configEntry, error) {
	out, err := gitOut(dir, "config", "-z", "--get-regexp", `^comb\.`)
	if err != nil {
		if strings.Contains(err.Error(), "exit status 1") {
			return nil, nil
		}
		return nil, err
	}
	var entries []configEntry
	for _, record := range strings.Split(out, "\x00") {
		if record == "" {
			continue
		}
		key, value, _ := strings.Cut(record, "\n")
		entries = append(entries, configEntry{key: strings.ToLower(key), value: value})
	}
	return entries, nil
}

// gitBool parses the value spellings git itself accepts for booleans.
func gitBool(v string) (bool, error) {
	switch strings.ToLower(v) {
	case "true", "yes", "on", "1":
		return true, nil
	case "false", "no", "off", "0", "":
		return false, nil
	}
	return false, fmt.Errorf("invalid boolean %q", v)
}
