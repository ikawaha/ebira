#!/usr/bin/env bash
# serve.sh builds and starts the ebira server, with the API keys of the
# assistant (POST /ask) read from 1Password. Only 1Password secret references
# are written here and passed around; `op run` resolves them for the server
# process alone and masks the keys in its output.
#
# Usage: scripts/serve.sh [start] [flags of ebira serve...]
#   scripts/serve.sh -model gemini-3.8-flash   run the server in the foreground
#   scripts/serve.sh start                     start it in the background
#                                              (ebira server start), with the keys
#
# The TUI (ebira) uses a server already running for the data directory, so
# start the server with this script first to give the assistant its keys.
#
# Without -data-dir, the data of the repository is used: notes/ and .ebira/ at its
# top, both ignored by git.
#
# Environment:
#   EBIRA_GEMINI_REF     secret reference of the Gemini API key
#                        (default: op://Private/Gemini API/credential; set it
#                        empty to pass no Gemini key)
#   EBIRA_ANTHROPIC_REF  secret reference of the Anthropic API key (default:
#                        none). Not exported globally on purpose: Claude Code
#                        also uses ANTHROPIC_API_KEY when it finds one.
#
# Get the reference of a field with "Copy Secret Reference" in 1Password; the
# vault name (Private, Personal, ...) depends on the account.
set -euo pipefail

# CDPATH is cleared so that cd does not print the directory into repo.
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
bin=${XDG_CACHE_HOME:-$HOME/.cache}/ebira/ebira

if ! command -v op >/dev/null 2>&1; then
	echo "serve.sh: the 1Password CLI (op) is not installed: brew install 1password-cli" >&2
	exit 1
fi

gemini_ref=${EBIRA_GEMINI_REF-"op://Private/Gemini API/credential"}
anthropic_ref=${EBIRA_ANTHROPIC_REF-}
vars=()
if [ -n "$gemini_ref" ]; then
	vars+=("GEMINI_API_KEY=$gemini_ref")
fi
if [ -n "$anthropic_ref" ]; then
	vars+=("ANTHROPIC_API_KEY=$anthropic_ref")
fi
if [ ${#vars[@]} -eq 0 ]; then
	echo "serve.sh: no secret reference is set; the assistant will be disabled" >&2
fi

# Build outside the repository and run from the current directory, so that a
# relative -data-dir is relative to where the script is called.
mkdir -p "$(dirname "$bin")"
go -C "$repo" build -o "$bin" ./cmd/ebira

cmd=(serve)
if [ "${1-}" = start ]; then
	cmd=(server start)
	shift
fi

has_data=false
for arg in "$@"; do
	case $arg in
	-data-dir | -data-dir=* | --data-dir | --data-dir=*) has_data=true ;;
	esac
done
if ! $has_data; then
	set -- -data-dir "$repo" "$@"
fi

# ${vars[@]+...} keeps an empty array from failing under set -u in bash 3.2,
# the version macOS ships.
exec env ${vars[@]+"${vars[@]}"} op run -- "$bin" "${cmd[@]}" "$@"
