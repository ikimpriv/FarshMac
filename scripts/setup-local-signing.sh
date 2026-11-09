#!/bin/sh
# One-time local signing identity. No Apple account, system keychain, or TLS trust.
set -eu
[ "$(uname -s)" = Darwin ] || { echo 'Requires macOS.' >&2; exit 1; }
[ "$#" -eq 0 ] || { echo 'Usage: ./scripts/setup-local-signing.sh' >&2; exit 64; }
name='FarshMac Local Development'
keychain="$HOME/Library/Keychains/login.keychain-db"
if security find-certificate -c "$name" "$keychain" >/dev/null 2>&1; then
  echo 'The local certificate already exists; it will not be replaced.'
  identities=$(security find-identity -v -p codesigning "$keychain")
  printf '%s\n' "$identities"
  if ! printf '%s\n' "$identities" | grep -F '"FarshMac Local Development"' >/dev/null; then
    echo 'Existing certificate is unusable. Check its private key, expiry, and Code Signing trust in Keychain Access.' >&2
    exit 1
  fi
  exit 0
fi
umask 077
staging=$(mktemp -d "${TMPDIR:-/tmp}/farshmac-signing.XXXXXX")
trap 'rm -rf "$staging"' EXIT HUP INT TERM
cat > "$staging/openssl.cnf" <<'CONFIG'
[req]
prompt = no
distinguished_name = subject
x509_extensions = signing
[subject]
CN = FarshMac Local Development
[signing]
basicConstraints = critical,CA:false
keyUsage = critical,digitalSignature
extendedKeyUsage = critical,codeSigning
subjectKeyIdentifier = hash
CONFIG
# The private key is temporary; its persistent copy lives only in login Keychain.
openssl req -new -newkey rsa:3072 -nodes -x509 -days 3650 \
  -config "$staging/openssl.cnf" -keyout "$staging/key.pem" -out "$staging/cert.pem" 2>"$staging/generation.log"
openssl rand -base64 32 > "$staging/password"
openssl pkcs12 -export -inkey "$staging/key.pem" -in "$staging/cert.pem" \
  -name "$name" -keypbe PBE-SHA1-3DES -certpbe PBE-SHA1-3DES -macalg sha1 \
  -out "$staging/identity.p12" -passout "file:$staging/password"
# Permit codesign only, not arbitrary applications (-A). Keychain may request consent.
security import "$staging/identity.p12" -k "$keychain" -P "$(cat "$staging/password")" -T /usr/bin/codesign
# User-domain trust is restricted to code signing; never trust this for TLS or system-wide.
security add-trusted-cert -r trustRoot -p codeSign -k "$keychain" "$staging/cert.pem"
security find-identity -v -p codesigning "$keychain"
printf '%s\n' 'Local identity installed. Builds will reuse it automatically.' \
  'Keep this certificate and private key: recreating them changes the app identity.' \
  'After the first signed build, grant Full Disk Access to that app once more.'
