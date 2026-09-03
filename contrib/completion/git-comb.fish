# Fish completion for git-comb and its `git comb` spelling.

function __git_comb_using_command
	set -l words (commandline -opc)
	if test "$words[1]" = git-comb
		return 0
	end
	if test "$words[1]" != git
		return 1
	end
	if functions -q __fish_git_using_command
		__fish_git_using_command comb
		return $status
	end
	test (count $words) -ge 2; and test "$words[2]" = comb
end

function __git_comb_signs
	set -l current (commandline -ct)
	set current (string replace -r '^(--only=|--except=|-o|-x)' '' -- "$current")
	if test -n "$current"
		printf '%s\t%s\n' "$current" (__git_comb_sign_description "$current")
	end
	for item in D:d U:u A:a B:b S:s E:e L:l O:o
		set -l fields (string split -m 1 : -- "$item")
		if not string match -q "*$fields[1]*" -- "$current"; and not string match -q "*$fields[2]*" -- "$current"
			set -l candidate "$current$fields[1]"
			printf '%s\t%s\n' "$candidate" (__git_comb_sign_description "$candidate")
		end
	end
end

function __git_comb_sign_description --argument-names value
	set -l meanings
	for sign in (string split '' -- "$value")
		switch "$sign"
			case D d
				set -a meanings 'uncommitted changes'
			case U u
				set -a meanings 'unpushed commits'
			case A a
				set -a meanings 'branches ahead of their upstream'
			case B b
				set -a meanings 'branches behind their upstream'
			case S s
				set -a meanings stashes
			case E e
				set -a meanings 'empty repositories'
			case L l
				set -a meanings 'repositories without remotes'
			case O o
				set -a meanings 'unreachable remotes'
		end
	end
	switch (count $meanings)
		case 0
			return
		case 1
			printf '%s\n' "$meanings[1]"
		case 2
			printf '%s or %s\n' "$meanings[1]" "$meanings[2]"
		case '*'
			printf '%s, or %s\n' (string join ', ' -- $meanings[1..-2]) "$meanings[-1]"
	end
end

for command in git-comb git
	complete -c $command -n __git_comb_using_command -s s -l short -d 'Show signs and paths, one repository per line'
	complete -c $command -n __git_comb_using_command -s a -l all -d 'Print clean repositories too'
	complete -c $command -n __git_comb_using_command -l only-dirty -d 'Look only for repositories with uncommitted changes'
	complete -c $command -n __git_comb_using_command -l only-unpushed -d 'Look only for commits that exist on no remote'
	complete -c $command -n __git_comb_using_command -l only-ahead -d 'Look only for branches ahead of their upstream'
	complete -c $command -n __git_comb_using_command -l only-behind -d 'Look only for branches behind their upstream'
	complete -c $command -n __git_comb_using_command -l only-stashed -d 'Look only for repositories with stashes'
	complete -c $command -n __git_comb_using_command -l only-empty -d 'Look only for empty repositories'
	complete -c $command -n __git_comb_using_command -l only-local -d 'Look only for repositories without remotes'
	complete -c $command -n __git_comb_using_command -l only-offline -d 'Look only for remotes unreachable during --fetch'
	complete -c $command -n __git_comb_using_command -f -s o -l only -r -a '(__git_comb_signs)' -d 'Combine sign classes using compact initials'
	complete -c $command -n __git_comb_using_command -f -s x -l except -r -a '(__git_comb_signs)' -d 'Exclude sign classes using compact initials'
	complete -c $command -n __git_comb_using_command -f -s j -l jobs -r -a '1 2 4 8 16' -d 'Set repository probe parallelism'
	complete -c $command -n __git_comb_using_command -l fetch -d 'Fetch all remotes first'
	complete -c $command -n __git_comb_using_command -l hidden -d 'Descend into hidden directories'
	complete -c $command -n __git_comb_using_command -l prune -r -a '(__fish_complete_directories (commandline -ct))' -d 'Skip directories matching a glob'
	complete -c $command -n __git_comb_using_command -l no-ignores -d 'Disregard configured acknowledgments'
	complete -c $command -n __git_comb_using_command -f -l color -r -a 'auto always never' -d 'Control colored output'
	complete -c $command -n __git_comb_using_command -l version -d 'Print the version and exit'
	complete -c $command -n __git_comb_using_command -s h -l help -d 'Show help'
	complete -c $command -n __git_comb_using_command -f -a '(__fish_complete_directories (commandline -ct))'
end
