#!/bin/sh
# Builds build/windows-<arch>/ebira: the desktop app for Windows (GOARCH, amd64
# by default) with the ebira server beside it, a folder to zip and hand out.
# It runs on macOS and Linux too, as the app needs no cgo on Windows. Windows
# 11 has the WebView2 runtime the app needs; Windows 10 may need it installed.
set -eu

cd "$(dirname "$0")"
arch=${GOARCH:-amd64}
out="build/windows-$arch/ebira"

rm -rf "$out"
mkdir -p "$out"
(cd ../.. && GOOS=windows GOARCH="$arch" CGO_ENABLED=0 go build -o "apps/desktop/$out/ebira.exe" ./cmd/ebira)
# The version of the app is the one of the server beside it, built from the
# same checkout (see build.sh).
version=$(go version -m "$out/ebira.exe" | awk '$1 == "mod" { print $3; exit }')
GOOS=windows GOARCH="$arch" CGO_ENABLED=0 go build -ldflags "-H windowsgui -X main.buildVersion=$version" -o "$out/ebira-desktop.exe" .
# The license of the icon of the web UI, which is not the license of ebira.
cp ../../internal/ui/web/static/favicon.LICENSE "$out/"
echo "$out"
