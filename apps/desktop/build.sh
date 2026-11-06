#!/bin/sh
# Builds build/ebira.app: the desktop app on Wails, with the ebira server of this
# repository in Contents/Helpers. Signed ad hoc, for this Mac only.
set -eu

cd "$(dirname "$0")"
app="build/ebira.app"

rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Helpers" "$app/Contents/Resources"
cp Info.plist "$app/Contents/"
# The icon of the about panel is the one of the web UI, with its license (CC BY
# 4.0, not the license of ebira).
cp ../../internal/ui/web/static/favicon.svg ../../internal/ui/web/static/favicon.LICENSE "$app/Contents/Resources/"
go build -o "$app/Contents/MacOS/ebira-desktop" .
(cd ../.. && go build -o "apps/desktop/$app/Contents/Helpers/ebira" ./cmd/ebira)

# The version of the app is the one of the server in it, built from the same
# checkout: v0.4.0, or a pseudo-version such as v0.4.1-0.20261106000000-abcdef123456+dirty.
# The bundle takes its numbers, and the count of commits as the build.
version=$("$app/Contents/Helpers/ebira" version | sed -n 's/^version: *//p')
short=$(printf '%s\n' "$version" | sed -nE 's/^v([0-9]+\.[0-9]+\.[0-9]+).*/\1/p')
plist="$app/Contents/Info.plist"
plutil -replace CFBundleShortVersionString -string "${short:-0.0.0}" "$plist"
plutil -replace CFBundleVersion -string "$(git rev-list --count HEAD 2>/dev/null || echo 0)" "$plist"
plutil -replace EbiraVersion -string "$version" "$plist"

codesign --force --sign - "$app/Contents/Helpers/ebira"
codesign --force --sign - "$app"
echo "$app"
