# Building and publishing FarshMac

The public repository is [ikimpriv/FarshMac](https://github.com/ikimpriv/FarshMac). Ordinary CI runs checks without signing credentials. Version tags run the signed release pipeline described below.

## Build a release artifact

Update the version and build number in `scripts/build.sh`, the displayed version in `internal/ui/draw.go`, README files, and CHANGELOG before a new version.

```sh
./scripts/check.sh --race
./scripts/package.sh
```

The package script builds a universal app, verifies its signature and both architectures, and writes:

```text
build/FarshMac-0.19.1-macOS-universal.zip
build/FarshMac-0.19.1-macOS-universal.zip.sha256
```

The version is read from the built app, so later versions get matching filenames. LICENSE and THIRD_PARTY_NOTICES.txt are included inside the application bundle. Runtime tests execute on the host architecture; universal compilation alone does not prove runtime behavior on the other architecture or the oldest supported macOS version. Test those separately before claiming support in a release announcement.

## Signing and notarization

CI uses ad-hoc signing by default. Local builds prefer the installed `FarshMac Local Development` identity, falling back to ad-hoc only when no local identity exists. An unusable existing local identity stops the build instead of silently changing signing identity. They are not Developer ID signed or notarized; do not describe them as such in a release. macOS Gatekeeper and privacy prompts can differ from those for a notarized release. Stable signing identity and bundle identifier matter for permissions across updates.

To use a signing identity already installed in your login keychain:

```sh
FARSHMAC_SIGN_IDENTITY='Developer ID Application: Your Name (TEAMID)' ./scripts/package.sh
```

No certificates, signing passwords, Apple credentials, or keychain profiles belong in Git. For a notarized distribution, perform Apple's signing/notarization process separately, validate the result, and repackage the final stapled app with an updated SHA-256 checksum. Only the signed release workflow imports certificate secrets; ordinary PR CI never receives them.

The current bundle identifier is `local.farshmac.app`. Keep this identifier stable across updates; changing it affects macOS permissions. The application is not configured for the Mac App Store sandbox.

## Tag and release

After reviewing and committing the exact release source:

```sh
git tag -a v0.19.1 -m "FarshMac 0.19.1"
git push origin v0.19.1
```

Before tagging, add `docs/releases/vVERSION.md` with release notes and commit the release source to `main`. The signed pipeline verifies that the tag is an ancestor of `main` and matches the bundle version, runs tests, signs a universal app, and verifies its checksum. A separate job publishes the ZIP and checksum to GitHub Releases. Versions beginning with `0.` are marked as prereleases. Existing releases are never overwritten. Manual workflow runs create artifacts without publishing.

## Stable local signing without a paid Apple account

Run once on your Mac:

```sh
./scripts/setup-local-signing.sh
./scripts/build.sh --universal
```

The setup script creates a self-signed RSA code-signing certificate valid for ten
years, imports its private key into login Keychain, and adds user-domain trust for
**code signing only**. It does not modify the system keychain or TLS trust. Keychain
may request approval. Temporary key/export files are removed on exit; the persistent
private key lives in Keychain, never in the repository. Only `/usr/bin/codesign` is
added to the imported key's application access list. Existing certificates are not
replaced. Keep the certificate and key across rebuilds; deleting/recreating them or
allowing them to expire requires a new identity and another permission grant.

Builds select the complete certificate name and sign by fingerprint. An explicit
`FARSHMAC_SIGN_IDENTITY` overrides this choice. After switching from ad-hoc signing,
quit the app, add the new copy to Full Disk Access once, and reopen it. Rebuilds
using the same certificate and bundle ID retain a stable designated requirement;
this does not itself grant access or bypass other macOS restrictions. Verify with:

```sh
codesign -d -r- 'build/FarshMac.app'
codesign --verify --deep --strict 'build/FarshMac.app'
```

Apple documents [self-signed identities for local development](https://developer.apple.com/library/archive/documentation/Security/Conceptual/CodeSigningGuide/Procedures/Procedures.html)
and explains why [ad-hoc identities cause repeated privacy prompts](https://developer.apple.com/forums/thread/678819).
Self-signed local builds are not Developer ID releases and cannot be notarized as
such. Use a Developer ID identity for public distribution. TCC persistence needs
manual verification across two launches after the user grants access; signature
checks alone do not prove that permission has already been granted.

## Moving signed builds to GitHub Actions

Two independent workflows are provided:

- `macOS checks and build` runs on main pushes/PRs without signing secrets and
  produces ad-hoc artifacts for testing.
- `Signed macOS release` runs manually on `main` or on `v*` tags, uses the `signing` environment,
  tests before importing credentials, and produces a signed ZIP/checksum. Tags
  publish a release after validation. Notarization is not performed.

Before enabling signed builds, create the GitHub environment **signing**. Restrict
its deployment branches to `main` and deployment tags to `v*`; configure required reviewers where your
GitHub plan supports them. Protect `main` and review changes to workflows/build
scripts before merging. The environment restriction matters even though the
workflow also checks the branch: workflow YAML on another branch can be edited.
Do not place the certificate in ordinary PR workflow environments or repository
files. The private key belongs only in encrypted environment secrets, never in repository contents or public artifacts.

For continuity with local builds, export **the existing** `FarshMac Local Development`
identity (certificate **and private key**) from Keychain Access → login → My
Certificates as a password-protected `.p12`. Do not create a new identity on every
runner. Save it outside the repository. Add these **environment secrets**:

| Secret | Value |
| --- | --- |
| `FARSHMAC_CERTIFICATE_BASE64` | Base64 of the encrypted `.p12` export |
| `FARSHMAC_CERTIFICATE_PASSWORD` | Its export password |
| `FARSHMAC_SIGN_IDENTITY` | The certificate's 40-character SHA-1 fingerprint |

Find the fingerprint with `security find-identity -v -p codesigning`. To copy an
export's base64 directly to the clipboard without printing it in terminal output:

```sh
/usr/bin/base64 -i /absolute/path/outside/repository/FarshMac-signing.p12 | pbcopy
```

Paste into the corresponding GitHub secret, then clear the clipboard. Keep a
secure backup of the identity/export and password; do not attach them to releases,
issues, or build artifacts. Never regenerate the certificate just because the
runner is new.

Open Actions → **Signed macOS release** → Run workflow → `main`. Keep **self_signed**
enabled for the local certificate. The script imports it into a temporary Keychain
on the disposable hosted runner, enables only code-signing trust there, signs, and
cleans up the keychain/export/trust on normal completion or failure. Runner disposal
is the final cleanup if the job is forcibly terminated. The script refuses to run
on your workstation or on a self-hosted runner.

Using the same certificate and `local.farshmac.app` keeps the designated requirement
consistent with local builds. The user still grants Full Disk Access explicitly.
Downloading an artifact introduces Gatekeeper/quarantine checks independently of
TCC; a self-signed build is not a notarized public release. For public distribution,
replace the secrets with a Developer ID identity, disable **self_signed**, and add
Apple notarization/stapling separately. Switching identities requires granting
permissions again once.

The workflow follows GitHub's [certificate import approach](https://docs.github.com/en/actions/how-tos/deploy/deploy-to-third-party-platforms/sign-xcode-applications).
Its YAML and shell syntax can be checked locally; a real hosted run requires a
remote repository, environment configuration, and the secrets. Repository setup and secret provisioning are separate from running the workflow.
