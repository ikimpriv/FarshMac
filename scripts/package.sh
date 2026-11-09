#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
[ "$#" -eq 0 ] || { printf '%s\n' 'Usage: ./scripts/package.sh' >&2; exit 64; }
./scripts/build.sh --universal
app='build/FarshMac.app'
version=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$app/Contents/Info.plist")
# Only a numeric version is used in output paths.
case "$version" in ''|*[!0-9.]*) printf '%s\n' 'Invalid bundle version' >&2; exit 1;; esac
lipo "$app/Contents/MacOS/farshmac" -verify_arch arm64
lipo "$app/Contents/MacOS/farshmac" -verify_arch x86_64
codesign --verify --deep --strict "$app"
archive="FarshMac-${version}-macOS-universal.zip"
staging=$(mktemp -d build/.farshmac-package.XXXXXX)
trap 'rm -rf "$staging"' EXIT
ditto -c -k --sequesterRsrc --keepParent "$app" "$staging/$archive"
mv "$staging/$archive" "build/$archive"
(cd build && shasum -a 256 "$archive" > "$archive.sha256")
printf 'Ready: build/%s\nSHA256: build/%s.sha256\n' "$archive" "$archive"
