#!/bin/bash
# Run only on a disposable GitHub-hosted macOS runner, after testing trusted code.
set -euo pipefail
[[ "${GITHUB_ACTIONS:-}" == true && "${RUNNER_ENVIRONMENT:-}" == github-hosted && "$(uname -s)" == Darwin ]] || {
  echo 'This script requires a disposable GitHub-hosted macOS runner.' >&2; exit 1;
}
: "${RUNNER_TEMP:?Missing runner temporary directory}"
: "${FARSHMAC_CERTIFICATE_BASE64:?Missing certificate secret}"
: "${FARSHMAC_CERTIFICATE_PASSWORD:?Missing certificate password}"
: "${FARSHMAC_SIGN_IDENTITY:?Missing certificate SHA-1 fingerprint}"
[[ "$FARSHMAC_SIGN_IDENTITY" =~ ^[[:xdigit:]]{40}$ ]] || {
  echo 'Signing identity must be the 40-character certificate SHA-1 fingerprint.' >&2; exit 1;
}
umask 077
signing_dir=$(mktemp -d "$RUNNER_TEMP/farshmac-signing.XXXXXX")
signing_keychain="$signing_dir/build.keychain-db"
trusted_certificate=false
# Preserve the runner's existing keychain search list. Never touch login private keys.
old_keychains=()
while IFS= read -r item; do
  item="${item#*\"}"; item="${item%\"*}"
  [[ -n "$item" ]] && old_keychains+=("$item")
done < <(security list-keychains -d user)
cleanup() {
  if [[ "$trusted_certificate" == true ]]; then
    security remove-trusted-cert "$signing_dir/certificate.pem" >/dev/null 2>&1 || true
  fi
  security list-keychains -d user -s "${old_keychains[@]}" >/dev/null 2>&1 || true
  security delete-keychain "$signing_keychain" >/dev/null 2>&1 || true
  rm -rf "$signing_dir"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
printf '%s' "$FARSHMAC_CERTIFICATE_BASE64" | /usr/bin/base64 --decode > "$signing_dir/certificate.p12"
unset FARSHMAC_CERTIFICATE_BASE64
keychain_password=$(openssl rand -hex 32)
security create-keychain -p "$keychain_password" "$signing_keychain"
security set-keychain-settings -lut 21600 "$signing_keychain"
security unlock-keychain -p "$keychain_password" "$signing_keychain"
security import "$signing_dir/certificate.p12" -k "$signing_keychain" \
  -P "$FARSHMAC_CERTIFICATE_PASSWORD" -T /usr/bin/codesign >/dev/null
security set-key-partition-list -S apple-tool:,apple:,codesign: -s \
  -k "$keychain_password" "$signing_keychain" >/dev/null
security list-keychains -d user -s "$signing_keychain" "${old_keychains[@]}"
if [[ "${FARSHMAC_SELF_SIGNED:-false}" == true ]]; then
  # Import trust only on this disposable runner and only for code signing.
  /usr/bin/openssl pkcs12 -in "$signing_dir/certificate.p12" -nokeys -clcerts \
    -passin env:FARSHMAC_CERTIFICATE_PASSWORD -out "$signing_dir/certificate.pem"
  cert_hash=$(/usr/bin/openssl x509 -in "$signing_dir/certificate.pem" -noout -fingerprint -sha1 | cut -d= -f2 | tr -d ':')
  [[ "$(printf '%s' "$cert_hash" | tr '[:lower:]' '[:upper:]')" == "$(printf '%s' "$FARSHMAC_SIGN_IDENTITY" | tr '[:lower:]' '[:upper:]')" ]] || {
    echo 'Certificate fingerprint does not match the configured signing identity.' >&2; exit 1;
  }
  trusted_certificate=true
  security add-trusted-cert -r trustRoot -p codeSign -k "$signing_keychain" "$signing_dir/certificate.pem"
fi
unset FARSHMAC_CERTIFICATE_PASSWORD keychain_password
rm -f "$signing_dir/certificate.p12"
export FARSHMAC_SIGN_IDENTITY
cd "$(dirname "$0")/.."
./scripts/package.sh
codesign -d -r- 'build/FarshMac.app'
