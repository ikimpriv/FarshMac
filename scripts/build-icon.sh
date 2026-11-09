#!/bin/sh
# Convert the unchanged upstream artwork into Apple's icon container.
set -eu
cd "$(dirname "$0")/.."
staging=$(mktemp -d "${TMPDIR:-/tmp}/farshmac-icon.XXXXXX")
trap 'rm -rf "$staging"' EXIT
mkdir "$staging/FarshMac.iconset"
sips -s format png resources/upstream/Far.ico --out "$staging/source.png" >/dev/null
for size in 16 32 128 256 512; do
 sips -z "$size" "$size" "$staging/source.png" --out "$staging/FarshMac.iconset/icon_${size}x${size}.png" >/dev/null
 double=$((size * 2))
 sips -z "$double" "$double" "$staging/source.png" --out "$staging/FarshMac.iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$staging/FarshMac.iconset" -o resources/FarshMac.icns
