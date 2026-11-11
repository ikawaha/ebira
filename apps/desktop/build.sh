#!/bin/sh
# Builds build/ebira.app: the desktop app on Wails, with the ebira server of this
# repository in Contents/Helpers. Signed ad hoc, for this Mac only.
set -eu

cd "$(dirname "$0")"
app="build/ebira.app"

# The oldest macOS the app runs on: that of the Go runtime (Go 1.27 runs on
# macOS 13). Wails and the Objective-C of the app work on older ones. It is
# also LSMinimumSystemVersion in Info.plist; change both together. The C and
# Objective-C of the packages that set no version of their own would be built
# for the macOS of the SDK, and the link would take Wails's 10.13: the
# compiler and the linker are given this version, and the linker is told not
# to warn of -lobjc, which go adds once for each package with Objective-C.
macos=13.0
export MACOSX_DEPLOYMENT_TARGET="$macos"
export CGO_CFLAGS="-O2 -g -mmacosx-version-min=$macos"
test "$(plutil -extract LSMinimumSystemVersion raw Info.plist)" = "$macos" # Info.plist differs

rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Helpers" "$app/Contents/Resources"
cp Info.plist "$app/Contents/"
# The icon of the app is the one of the web UI, with its license (CC BY 4.0, not
# the license of ebira). The picture of the favicon fills its square; a Mac icon
# keeps a margin, 824 of 1024 pixels, so the view of the SVG is widened by it.
static=../../internal/ui/web/static
cp "$static/favicon.LICENSE" "$app/Contents/Resources/"
icons=$(mktemp -d)
trap 'rm -rf "$icons"' EXIT
sed 's/viewBox="0 0 140 140"/viewBox="-17 -17 174 174"/' "$static/favicon.svg" >"$icons/icon.svg"
grep -q 'viewBox="-17 -17 174 174"' "$icons/icon.svg" # the favicon changed its view
mkdir "$icons/icon.iconset"
for size in 16 32 128 256 512; do
	sips -s format png -Z "$size" "$icons/icon.svg" --out "$icons/icon.iconset/icon_${size}x${size}.png" >/dev/null
	sips -s format png -Z "$((size * 2))" "$icons/icon.svg" --out "$icons/icon.iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns -o "$app/Contents/Resources/icon.icns" "$icons/icon.iconset"
go build -ldflags "-X main.bundled=true -extldflags '-mmacosx-version-min=$macos -Wl,-no_warn_duplicate_libraries'" -o "$app/Contents/MacOS/ebira-desktop" .
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
