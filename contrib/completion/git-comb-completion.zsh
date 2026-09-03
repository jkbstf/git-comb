# Zsh completion for git-comb.
#
# Source this after compinit. The _git_comb function is also discovered by
# Git's own completion when git-comb is invoked as `git comb`.

_git_comb_signs ()
{
	local typed=$PREFIX sign description
	local width=0 i
	local -a signs choices descriptions displays
	signs=(D U A B S E L O)
	choices=()
	for sign in $signs; do
		if [[ $typed != *${sign:l}* && $typed != *${sign:u}* ]]; then
			description=$(_git_comb_sign_description "$typed$sign")
			choices+=("$typed$sign")
			descriptions+=("$description")
		fi
	done
	if [[ -n $typed ]]; then
		description=$(_git_comb_sign_description "$typed")
		choices=("$typed" $choices)
		descriptions=("$description" $descriptions)
	fi
	for description in $choices; do
		(( ${#description} > width )) && width=${#description}
	done
	for (( i = 1; i <= ${#choices}; i++ )); do
		displays+=("${(r:$width:)choices[$i]}  -- $descriptions[$i]")
	done
	compadd -Q -S '' -d displays -a choices
}

_git_comb_sign_description ()
{
	local value=$1 sign i
	local -a meanings
	for (( i = 1; i <= ${#value}; i++ )); do
		sign=${value[$i]}
		case ${sign:u} in
			D) meanings+=('uncommitted changes') ;;
			U) meanings+=('unpushed commits') ;;
			A) meanings+=('branches ahead of their upstream') ;;
			B) meanings+=('branches behind their upstream') ;;
			S) meanings+=(stashes) ;;
			E) meanings+=('empty repositories') ;;
			L) meanings+=('repositories without remotes') ;;
			O) meanings+=('unreachable remotes') ;;
		esac
	done
	case ${#meanings} in
		0) ;;
		1) print -r -- "$meanings[1]" ;;
		2) print -r -- "$meanings[1] or $meanings[2]" ;;
		*) print -r -- "${(j:, :)meanings[1,-2]}, or $meanings[-1]" ;;
	esac
}

_git_comb_options ()
{
	local spec option description width=0 i
	local -a specs options descriptions displays
	specs=(
		'--short:show signs and paths, one repository per line'
		'--all:print clean repositories too'
		'--only-dirty:look only for repositories with uncommitted changes'
		'--only-unpushed:look only for commits that exist on no remote'
		'--only-ahead:look only for branches ahead of their upstream'
		'--only-behind:look only for branches behind their upstream'
		'--only-stashed:look only for repositories with stashes'
		'--only-empty:look only for empty repositories'
		'--only-local:look only for repositories without remotes'
		'--only-offline:look only for remotes unreachable during --fetch'
		'--only:combine sign classes using compact initials'
		'--except:exclude sign classes using compact initials'
		'--jobs:set repository probe parallelism'
		'--fetch:fetch all remotes first'
		'--hidden:descend into hidden directories'
		'--prune:skip directories matching a glob'
		'--no-ignores:disregard configured acknowledgments'
		'--color:control colored output'
		'--version:print the version and exit'
		'--help:show help'
	)
	for spec in $specs; do
		option=${spec%%:*}
		description=${spec#*:}
		options+=("$option")
		descriptions+=("$description")
		(( ${#option} > width )) && width=${#option}
	done
	for (( i = 1; i <= ${#options}; i++ )); do
		displays+=("${(r:$width:)options[$i]}  -- $descriptions[$i]")
	done
	compadd -Q -S ' ' -d displays -a options
}

_git_comb_values ()
{
	local -a values
	values=("$@")
	compadd -Q -S ' ' -a values
}

_git_comb ()
{
	emulate -L zsh
	# Git's bundled Zsh adapter supplies Bash-style cur and prev variables but
	# may leave the native words array shifted past the subcommand. Restore the
	# minimal context that native completion helpers expect.
	if [[ ${words[CURRENT]-} != $cur ]]; then
		words=(git-comb "$cur")
		CURRENT=2
	fi
	case $prev in
		--color)
			_git_comb_values auto always never
			return
			;;
		--jobs|-j)
			_git_comb_values 1 2 4 8 16
			return
			;;
		--only|-o|--except|-x)
			_git_comb_signs
			return
			;;
		--prune)
			_directories
			return
			;;
	esac

	case $cur in
		--color=*)
			compset -P '--color='
			_git_comb_values auto always never
			;;
		--jobs=*)
			compset -P '--jobs='
			_git_comb_values 1 2 4 8 16
			;;
		--only=*|--except=*)
			compset -P '*='
			_git_comb_signs
			;;
		--prune=*)
			compset -P '--prune='
			_directories
			;;
		-o?*|-x?*)
			compset -P '-[ox]'
			_git_comb_signs
			;;
		--*)
			_git_comb_options
			;;
		*)
			_directories
			;;
	esac
}

_git_comb_direct ()
{
	_arguments -s -S \
		'(-s --short)'{-s,--short}'[show signs and paths, one repository per line]' \
		'(-a --all)'{-a,--all}'[print clean repositories too]' \
		'--only-dirty[look only for repositories with uncommitted changes]' \
		'--only-unpushed[look only for commits that exist on no remote]' \
		'--only-ahead[look only for branches ahead of their upstream]' \
		'--only-behind[look only for branches behind their upstream]' \
		'--only-stashed[look only for repositories with stashes]' \
		'--only-empty[look only for empty repositories]' \
		'--only-local[look only for repositories without remotes]' \
		'--only-offline[look only for remotes unreachable during --fetch]' \
		'(-o --only)'{-o,--only}'[combine sign classes using compact initials]:signs:_git_comb_signs' \
		'(-x --except)'{-x,--except}'[exclude sign classes using compact initials]:signs:_git_comb_signs' \
		'(-j --jobs)'{-j,--jobs}'[set repository probe parallelism]:jobs:(1 2 4 8 16)' \
		'--fetch[fetch all remotes first]' \
		'--hidden[descend into hidden directories]' \
		'*--prune[skip directories matching a glob]:directory glob:_directories' \
		'--no-ignores[disregard configured acknowledgments]' \
		'--color[control colored output]:color mode:(auto always never)' \
		'--version[print the version and exit]' \
		'(-h --help)'{-h,--help}'[show help]' \
		'*:directory:_directories'
}

# Git's bundled Zsh completion dispatches external commands to _git_comb,
# while Zsh's own Git completion uses the hyphenated _git-comb name.
_git-comb ()
{
	_git_comb_direct "$@"
}

if (( $+functions[compdef] )); then
	compdef _git_comb_direct git-comb
fi
