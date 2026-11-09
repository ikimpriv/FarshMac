#!/bin/sh
set -eu
case "${1:-}" in ''|--universal) ;; *) printf '%s\n' 'Usage: ./scripts/build.sh [--universal]' >&2; exit 64;; esac
[ "$#" -le 1 ] || { printf '%s\n' 'Too many build arguments' >&2; exit 64; }
[ "$(uname -s)" = Darwin ] || { printf '%s\n' 'Build requires macOS and Xcode Command Line Tools.' >&2; exit 1; }
export CGO_ENABLED=1
cd "$(dirname "$0")/.."
export CGO_CFLAGS="${CGO_CFLAGS:--O2 -g} -mmacosx-version-min=12.0"
export CGO_LDFLAGS="${CGO_LDFLAGS:-} -mmacosx-version-min=12.0"
mkdir -p build
staging=$(mktemp -d build/.farshmac-build.XXXXXX)
trap 'rm -rf "$staging"' EXIT
bundle="$staging/FarshMac.app"
mkdir -p "$bundle/Contents/MacOS" "$bundle/Contents/Resources"
if [ "${1:-}" = "--universal" ]; then
  CGO_ENABLED=1 GOARCH=arm64 go build -trimpath -o "$staging/farshmac-arm64" ./cmd/farshmac
  CGO_ENABLED=1 GOARCH=amd64 go build -trimpath -o "$staging/farshmac-amd64" ./cmd/farshmac
  lipo -create "$staging/farshmac-arm64" "$staging/farshmac-amd64" -output "$bundle/Contents/MacOS/farshmac"
else
  go build -trimpath -o "$bundle/Contents/MacOS/farshmac" ./cmd/farshmac
fi
cp resources/FarshMac.icns "$bundle/Contents/Resources/"
cp LICENSE THIRD_PARTY_NOTICES.txt "$bundle/Contents/Resources/"
cat > "$bundle/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>farshmac</string>
<key>CFBundleIdentifier</key><string>local.farshmac.app</string>
<key>CFBundleName</key><string>FarshMac</string>
<key>CFBundleDisplayName</key><string>FarshMac</string>
<key>CFBundleIconFile</key><string>FarshMac.icns</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>0.20.0</string>
<key>CFBundleVersion</key><string>41</string>
<key>LSMinimumSystemVersion</key><string>12.0</string>
<key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST
# Keep using the same certificate and bundle identifier across releases so TCC
# can recognise updates. Never fall back silently if a selected identity fails.
sign_identity=${FARSHMAC_SIGN_IDENTITY:-}
if [ -z "$sign_identity" ] && [ "${CI:-}" != true ]; then
  # Match the complete certificate name, and sign by its unique fingerprint.
  sign_identity=$(security find-identity -v -p codesigning "$HOME/Library/Keychains/login.keychain-db" 2>/dev/null |
    awk '$3 == "\"FarshMac" && $4 == "Local" && $5 == "Development\"" {print $2}')
  case "$sign_identity" in *'
'*) echo 'Multiple local signing identities found; set FARSHMAC_SIGN_IDENTITY explicitly.' >&2; exit 1;; esac
  if [ -z "$sign_identity" ] && security find-certificate -c 'FarshMac Local Development' "$HOME/Library/Keychains/login.keychain-db" >/dev/null 2>&1; then
    echo 'Local signing certificate exists but is not usable. Unlock/fix it in Keychain; refusing ad-hoc fallback.' >&2
    exit 1
  fi
fi
if [ -n "$sign_identity" ]; then
  codesign --force --options runtime --sign "$sign_identity" "$bundle"
else
  printf '%s\n' 'Warning: ad-hoc signing; macOS privacy permissions may need to be granted again after rebuilding.' >&2
  codesign --force --sign - "$bundle"
fi
codesign --verify --deep --strict "$bundle"
# Keep the old executable inode intact for any running instance.
if [ -e 'build/FarshMac.app' ]; then
  previous=$(mktemp -d build/.farshmac-previous.XXXXXX)
  mv 'build/FarshMac.app' "$previous/FarshMac.previous"
fi
mv "$bundle" 'build/FarshMac.app'
printf 'Built: %s/build/FarshMac.app\n' "$PWD"
