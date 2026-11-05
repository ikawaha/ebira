#!/bin/sh
# Builds build/ebira.app: the desktop app on Wails, with the ebira server of this
# repository in Contents/Helpers. Signed ad hoc, for this Mac only.
set -eu

cd "$(dirname "$0")"
app="build/ebira.app"

rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Helpers"
cp Info.plist "$app/Contents/"
go build -o "$app/Contents/MacOS/ebira-desktop" .
(cd ../.. && go build -o "apps/desktop/$app/Contents/Helpers/ebira" ./cmd/ebira)

codesign --force --sign - "$app/Contents/Helpers/ebira"
codesign --force --sign - "$app"
echo "$app"
