# Bash completion for git-comb.
#
# Source this after Git's git-completion.bash. The _git_comb function is the
# extension point Git uses for external subcommands, while the registration at
# the bottom also completes the git-comb executable directly.

__git_comb_options='--short --all --only-dirty --only-unpushed --only-ahead --only-behind --only-stashed --only-empty --only-local --only-offline --exclude-dirty --exclude-unpushed --exclude-ahead --exclude-behind --exclude-stashed --exclude-empty --exclude-local --exclude-offline --only --except --jobs --fetch --hidden --max-depth --prune --no-ignores --color --diagnostics --version --help'

__git_comb_words ()
{
	local current=$1 values=$2 prefix=${3-} candidate
	COMPREPLY=()
	while IFS= read -r candidate; do
		COMPREPLY+=("$prefix$candidate")
	done < <(compgen -W "$values" -- "$current")
}

__git_comb_directories ()
{
	local current=$1 prefix=${2-} candidate
	COMPREPLY=()
	while IFS= read -r candidate; do
		COMPREPLY+=("$prefix$candidate")
	done < <(compgen -d -- "$current")
}

__git_comb_files ()
{
	local current=$1 prefix=${2-} candidate
	COMPREPLY=()
	while IFS= read -r candidate; do
		COMPREPLY+=("$prefix$candidate")
	done < <(compgen -f -- "$current")
}

__git_comb_signs ()
{
	local current=$1 prefix=${2-} sign
	COMPREPLY=()
	if [ -n "$current" ]; then
		COMPREPLY+=("$prefix$current")
	fi
	for sign in D U A B S E L O; do
		case "$sign:$current" in
			D:*D*|D:*d*|U:*U*|U:*u*|A:*A*|A:*a*|B:*B*|B:*b*|\
			S:*S*|S:*s*|E:*E*|E:*e*|L:*L*|L:*l*|O:*O*|O:*o*) ;;
			*) COMPREPLY+=("$prefix$current$sign") ;;
		esac
	done
}

_git_comb ()
{
	local current=${cur-} previous=${prev-} prefix value

	case "$previous" in
		--color)
			__git_comb_words "$current" 'auto always never'
			return
			;;
		--jobs|-j)
			__git_comb_words "$current" '1 2 4 8 16'
			return
			;;
		--max-depth)
			__git_comb_words "$current" '0 1 2 3 4 5'
			return
			;;
		--only|-o|--except|-x)
			__git_comb_signs "$current"
			return
			;;
		--prune)
			__git_comb_directories "$current"
			return
			;;
		--diagnostics)
			__git_comb_files "$current"
			return
			;;
	esac

	case "$current" in
		--color=*)
			prefix=--color=
			value=${current#*=}
			__git_comb_words "$value" 'auto always never' "$prefix"
			;;
		--jobs=*)
			prefix=--jobs=
			value=${current#*=}
			__git_comb_words "$value" '1 2 4 8 16' "$prefix"
			;;
		--max-depth=*)
			prefix=--max-depth=
			value=${current#*=}
			__git_comb_words "$value" '0 1 2 3 4 5' "$prefix"
			;;
		--only=*|--except=*)
			prefix=${current%%=*}=
			value=${current#*=}
			__git_comb_signs "$value" "$prefix"
			;;
		--prune=*)
			prefix=--prune=
			value=${current#*=}
			__git_comb_directories "$value" "$prefix"
			;;
		--diagnostics=*)
			prefix=--diagnostics=
			value=${current#*=}
			__git_comb_files "$value" "$prefix"
			;;
		-o?*|-x?*)
			prefix=${current:0:2}
			value=${current:2}
			__git_comb_signs "$value" "$prefix"
			;;
		--*)
			__git_comb_words "$current" "$__git_comb_options"
			;;
		*)
			__git_comb_directories "$current"
			;;
	esac
}

__git_comb_wrap ()
{
	local cur prev cword words
	words=("${COMP_WORDS[@]}")
	cword=$COMP_CWORD
	cur=${words[cword]}
	if [ "$cword" -gt 0 ]; then
		prev=${words[cword-1]}
	else
		prev=
	fi
	_git_comb
}

complete -o bashdefault -o default -o nospace -F __git_comb_wrap git-comb 2>/dev/null ||
	complete -o default -o nospace -F __git_comb_wrap git-comb
