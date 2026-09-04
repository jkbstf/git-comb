package comb

import (
	"fmt"
	"strconv"
	"strings"
)

// Settings are the scan-level defaults read from git config at
// startup. OnlyNamed and ExcludeNamed contain the canonical signs
// selected through descriptive boolean keys; Only and Except keep
// the advanced compact settings for backward compatibility.
type Settings struct {
	Prune        []string
	Jobs         int
	Hidden       bool
	Only         string
	OnlyNamed    string
	Except       string
	ExcludeNamed string
}

var namedOnlyConfigKeys = map[string]byte{
	"comb.onlydirty":    'D',
	"comb.onlyunpushed": 'U',
	"comb.onlyahead":    'A',
	"comb.onlybehind":   'B',
	"comb.onlystashed":  'S',
	"comb.onlyempty":    'E',
	"comb.onlylocal":    'L',
	"comb.onlyoffline":  'O',
}

var namedExcludeConfigKeys = map[string]byte{
	"comb.excludedirty":    'D',
	"comb.excludeunpushed": 'U',
	"comb.excludeahead":    'A',
	"comb.excludebehind":   'B',
	"comb.excludestashed":  'S',
	"comb.excludeempty":    'E',
	"comb.excludelocal":    'L',
	"comb.excludeoffline":  'O',
}

// LoadSettings reads comb.* scan defaults from the merged git config
// visible from dir — global and system config everywhere, plus the
// local config when dir sits inside a repository.
func LoadSettings(dir string) (Settings, error) {
	return LoadSettingsWithDiagnostics(dir, nil)
}

// LoadSettingsWithDiagnostics is LoadSettings with privacy-safe command
// timing enabled for a diagnostic run.
func LoadSettingsWithDiagnostics(dir string, diagnostics *Diagnostics) (Settings, error) {
	entries, err := configEntries(gitRunner{diagnostics: diagnostics}, dir)
	if err != nil {
		return Settings{}, err
	}
	var s Settings
	namedOnly := map[byte]bool{}
	namedExclude := map[byte]bool{}
	for _, e := range entries {
		if sign, ok := namedOnlyConfigKeys[e.key]; ok {
			b, err := gitBool(e.value)
			if err != nil {
				return Settings{}, fmt.Errorf("%s: %w", e.key, err)
			}
			namedOnly[sign] = b
			continue
		}
		if sign, ok := namedExcludeConfigKeys[e.key]; ok {
			b, err := gitBool(e.value)
			if err != nil {
				return Settings{}, fmt.Errorf("%s: %w", e.key, err)
			}
			namedExclude[sign] = b
			continue
		}
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
	s.OnlyNamed = configuredSigns(namedOnly)
	s.ExcludeNamed = configuredSigns(namedExclude)
	return s, nil
}

func configuredSigns(selected map[byte]bool) string {
	var signs strings.Builder
	for i := 0; i < len(signOrder); i++ {
		if selected[signOrder[i]] {
			signs.WriteByte(signOrder[i])
		}
	}
	return signs.String()
}

// repoIgnores reads a repository's acknowledgment config: comb.ignore
// silences the whole repository, comb.ignoreBranch globs acknowledge
// deliberately local-only branches. Both merge local over global, so
// a global glob applies everywhere and a local one to that clone.
func repoIgnores(git gitRunner, repo string) (ignored bool, globs []string, err error) {
	ignored, globs, _, err = repoSettings(git, repo, false)
	return ignored, globs, err
}

// repoSettings reads repository acknowledgments and, when requested, detects
// an ordinary configured remote in the same Git process. A positive remote
// result is conclusive; callers retain a git remote fallback for unusual
// layouts that have no remote.* config entry.
func repoSettings(git gitRunner, repo string, includeRemotes bool) (ignored bool, globs []string, configuredRemote bool, err error) {
	pattern := `^comb\.`
	if includeRemotes {
		pattern = `^(comb\.|remote\.)`
	}
	entries, err := configEntriesMatching(git, repo, pattern)
	if err != nil {
		return false, nil, false, err
	}
	for _, e := range entries {
		if remoteConfigEntry(e.key) {
			configuredRemote = true
			continue
		}
		switch e.key {
		case "comb.ignore":
			b, err := gitBool(e.value)
			if err != nil {
				return false, nil, false, fmt.Errorf("comb.ignore: %w", err)
			}
			ignored = b
		case "comb.ignorebranch": // git lowercases config key names
			globs = append(globs, e.value)
		}
	}
	return ignored, globs, configuredRemote, nil
}

func remoteConfigEntry(key string) bool {
	rest, ok := strings.CutPrefix(key, "remote.")
	return ok && strings.Contains(rest, ".")
}

type configEntry struct {
	key   string
	value string
}

// configEntries lists every comb.* config entry visible from dir.
// git exits 1 when nothing matches; that simply means no settings.
func configEntries(git gitRunner, dir string) ([]configEntry, error) {
	return configEntriesMatching(git, dir, `^comb\.`)
}

func configEntriesMatching(git gitRunner, dir, pattern string) ([]configEntry, error) {
	out, err := git.out(dir, "config", "config", "-z", "--get-regexp", pattern)
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
