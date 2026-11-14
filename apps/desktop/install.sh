#!/bin/sh
# Builds the desktop app and installs it in /Applications, or over the one
# installed before, until the app is distributed signed:
#
#   apps/desktop/install.sh [-user] [-no-open] [ref]
#
# In a checkout, it builds the checkout. Run from curl, or given a ref (a tag
# or a branch), it clones ebira at the ref, the latest release by default,
# into a temporary directory:
#
#   curl -fsSL https://raw.githubusercontent.com/ikawaha/ebira/main/apps/desktop/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/ikawaha/ebira/main/apps/desktop/install.sh | sh -s -- v0.8.1
#
# The app running is asked to quit first, which asks about a note being
# edited. The server the installed app started runs its ebira, so it is
# started again with the new one, with the same flags. -user installs in
# ~/Applications; an app installed before is replaced where it is.
set -eu

repo=https://github.com/ikawaha/ebira.git
id=io.github.ikawaha.ebira
dest=
open=yes
ref=

usage() {
	sed -n '2,17s/^# \{0,1\}//p' "$0" 2>/dev/null || echo "usage: install.sh [-user] [-no-open] [ref]"
}

while [ $# -gt 0 ]; do
	case $1 in
	-user) dest=$HOME/Applications ;;
	-no-open) open= ;;
	-h | -help | --help)
		usage
		exit 0
		;;
	-*)
		echo "install.sh: unknown flag $1" >&2
		exit 2
		;;
	*) ref=$1 ;;
	esac
	shift
done

fail() {
	echo "install.sh: $*" >&2
	exit 1
}

command -v go >/dev/null || fail "go is not found; install Go 1.27 or later from https://go.dev/dl/"
xcode-select -p >/dev/null 2>&1 || fail "the Xcode Command Line Tools are not installed; run: xcode-select --install"

# The checkout to build: the one of this script, or a clone.
here=$(cd "$(dirname "$0")" 2>/dev/null && pwd) || here=
if [ -z "$ref" ] && [ -n "$here" ] && [ -f "$here/build.sh" ] && grep -q "<string>$id</string>" "$here/Info.plist" 2>/dev/null; then
	src=$here
else
	command -v git >/dev/null || fail "git is not found"
	if [ -z "$ref" ]; then
		ref=$(git ls-remote --tags --refs "$repo" 'v*' | sed 's|.*refs/tags/||' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -n 1)
		[ -n "$ref" ] || fail "no release of ebira is found at $repo"
	fi
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT
	echo "Cloning ebira ${ref}..."
	# Not shallow: the build counts the commits for the version of the bundle.
	git -c advice.detachedHead=false clone -q --filter=blob:none --branch "$ref" "$repo" "$tmp/ebira"
	src=$tmp/ebira/apps/desktop
fi

echo "Building the app..."
"$src/build.sh" >/dev/null
app=$src/build/ebira.app

# Where to install: where it was installed before, or else /Applications.
if [ -z "$dest" ]; then
	dest=/Applications
	if [ ! -d /Applications/ebira.app ] && [ -d "$HOME/Applications/ebira.app" ]; then
		dest=$HOME/Applications
	fi
fi
mkdir -p "$dest"
[ -w "$dest" ] || fail "$dest is not writable; install in ~/Applications with -user"
installed=$dest/ebira.app
helper=$installed/Contents/Helpers/ebira

running() {
	[ "$(osascript -e "application id \"$id\" is running" 2>/dev/null)" = true ]
}
if running; then
	echo "Quitting ebira..."
	osascript -e 'ignoring application responses' -e "tell application id \"$id\" to quit" -e 'end ignoring' >/dev/null
	i=0
	while running; do
		i=$((i + 1))
		[ "$i" -le 120 ] || fail "ebira did not quit, such as for a note being edited; quit it and run this again"
		sleep 1
	done
fi

# The server of the notes of the app, if the installed app started it.
restart=
if [ -x "$helper" ]; then
	notes=$(defaults read "$id" notesDir 2>/dev/null) || notes=
	if [ -n "$notes" ]; then
		status=$("$helper" server status -json -notes "$notes" 2>/dev/null) || status=
	else
		status=$("$helper" server status -json 2>/dev/null) || status=
	fi
	pid=$(printf '%s' "$status" | plutil -extract state.pid raw -o - - 2>/dev/null) || pid=
	if [ -n "$pid" ] && [ "$(ps -o comm= -p "$pid" 2>/dev/null)" = "$helper" ]; then
		restart=$(printf '%s' "$status" | plutil -extract notes_dir raw -o - -)
	fi
fi

echo "Installing ${installed}..."
staged=$dest/.ebira.app.new
rm -rf "$staged"
ditto "$app" "$staged"
rm -rf "$installed"
mv "$staged" "$installed"

if [ -n "$restart" ]; then
	echo "Starting the server of ${restart} again with the new build..."
	"$helper" server restart -notes "$restart"
fi

if [ -n "$open" ]; then
	open "$installed"
fi
echo "Installed: $("$helper" version | sed -n 's/^version: *//p')"
