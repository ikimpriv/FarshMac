# FarshMac

**The two-panel workflow you know. A Mac window, a real shell, and Go underneath.**

[![macOS CI](https://github.com/ikimpriv/FarshMac/actions/workflows/ci.yml/badge.svg)](https://github.com/ikimpriv/FarshMac/actions/workflows/ci.yml)
[![Signed releases](https://github.com/ikimpriv/FarshMac/actions/workflows/signed-build.yml/badge.svg)](https://github.com/ikimpriv/FarshMac/actions/workflows/signed-build.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Platform: macOS](https://img.shields.io/badge/macOS-12%2B-black.svg)](https://github.com/ikimpriv/FarshMac/releases)

A native macOS file manager written in Go, inspired by the keyboard-driven workflow and appearance of [Far Manager](https://www.farmanager.com/).

**Version 0.20.0 · macOS 12+ · Apple Silicon and Intel**

[Changelog](CHANGELOG.md) · [File operations](docs/FILE_OPERATIONS.md) · [Contributing](CONTRIBUTING.md)

FarshMac uses a Cocoa window and an offline WebKit/xterm.js interface. It is an independent implementation, not the official Far Manager or a source-compatible port. Far Group does not endorse this project.

![FarshMac two-panel interface with sample files](docs/images/panels.svg)

*Interface preview rendered by FarshMac with sample files.*

## Download

[**Download the latest preview →**](https://github.com/ikimpriv/FarshMac/releases/tag/v0.20.0)

Unzip the universal macOS archive and move **FarshMac.app** to Applications. Apple Silicon and Intel executables are included. The release includes a SHA-256 checksum.

**Early preview:** builds use a persistent self-signed certificate, not Developer ID or notarization. macOS may require per-app approval in **System Settings → Privacy & Security**. Full Disk Access is a separate, optional permission for protected folders. See the [release notes](docs/releases/v0.20.0.md) for installation details and known limitations.

## Features

- Two file panels, masks and selection, folder bookmarks, sorting, directory sizes, and multiple windows with saved positions and folders.
- Persistent bash, sh, or zsh terminal with bidirectional folder synchronization, color, mouse support, and bounded scrollback spooled to temporary files.
- Internal viewer and editor, Chroma syntax highlighting, autoindent, search, and external application integration.
- ZIP, TAR, compressed TAR, and other archive formats; enter archives as folders and copy files out.
- FTP, FTPS, and SFTP panels, saved connection profiles, and configurable parallel copying.
- Trash by default, explicit permanent deletion, cancellable background operations, and copy metadata options.
- Graphical image/PDF quick view and an information panel with macOS disk, memory, and power data.

## Build and run

You need **macOS 12 or later**, **Xcode Command Line Tools**, and **Go 1.25 or later**. CGO is required. Node.js 22+ is needed only to run renderer tests. The app bundles its web assets; it does not download scripts at runtime.

Clone the repository, then build:

```sh
git clone https://github.com/ikimpriv/FarshMac.git
cd FarshMac
```

From the repository root:

```sh
xcode-select --install  # only if Command Line Tools are not installed
./scripts/build.sh
open "build/FarshMac.app"
```

For a universal application containing both architectures:

```sh
./scripts/build.sh --universal
```

For development with explicit starting folders:

```sh
go run ./cmd/farshmac -left "$HOME" -right /tmp
```

Local builds automatically reuse the `FarshMac Local Development` code-signing identity if installed; otherwise they use ad-hoc signing. Run `./scripts/setup-local-signing.sh` once to create that identity in login Keychain without an Apple Developer account. macOS may request Keychain approval. This provides stable local signing, but the app is **not notarized**. For local permission behavior, signing, and release packaging, see [Publishing](docs/PUBLISHING.md). Optional Full Disk Access must be enabled by the user in macOS System Settings; the app cannot grant it to itself.

## Keyboard essentials

| Key | Action |
| --- | --- |
| Tab | Switch active panel |
| Cmd+O | Switch between panels and terminal; synchronize the folder |
| Cmd+Option+F3 | Calculate all displayed folder sizes |
| F3 / F4 | View / edit; F3 on a folder calculates selected folder sizes |
| F5 / F6 / F7 | Copy / move / create folder |
| F8 | Move to Trash |
| Shift+F8 / Shift+Delete | Delete permanently |
| F9 | Open the menu |
| Cmd+Enter | Insert the selected filename into the command line |
| Shift+Enter | Open with macOS / Finder |
| Ctrl+Q / Ctrl+L | Toggle quick view / information in the other panel |
| Ctrl+Shift+1…9 / Ctrl+1…9 | Save / open folder bookmarks |
| Cmd+Shift+H | Shell command history: Enter to run, F4 to edit |
| Cmd+Q | Quit |

F1 opens the complete in-app keyboard reference. On keyboards configured for media keys, hold Fn to use F1–F12. Files → **Calculate all folder sizes** calculates all displayed folders. Calculated sizes participate in size sorting. They remain in memory when navigating back or refreshing; `~` marks a cached, possibly outdated value, and `>=` marks an incomplete scan. Recalculate with F3 or Cmd+Option+F3. Previous-directory snapshots are limited to 128 directories, 16,384 results, and a 2 MiB estimated storage budget per panel; least recently used snapshots expire. The current panel is separate from this history budget. Sizes are not saved across application restarts.

## Tests

```sh
./scripts/check.sh
./scripts/check.sh --race
```

The suite uses temporary directories and loopback FTP/SFTP servers. No real server credentials are needed. Tests that inspect a personal shell configuration or contact a real SFTP server are opt-in and excluded from the standard check script.

GitHub Actions runs checks and produces an ad-hoc universal ZIP artifact. A separate manual **Signed macOS release** workflow can reuse the same certificate as local builds through protected environment secrets; see [CI signing setup](docs/PUBLISHING.md#moving-signed-builds-to-github-actions). Version tags trigger the signed workflow, which publishes the verified ZIP and checksum after successful checks. Manual runs build artifacts without publishing.

## Scope and limitations

This is an early release. Far Manager plugin binaries and macros are not compatible. Archive panels are read-only; editing an archive member requires extracting it first. Encrypted and some nested archive workflows are not supported. Remote metadata and rename behavior depend on the server. Windows-specific information and actions are replaced with macOS equivalents.

See [archive support](ARCHIVE_SUPPORT.md), [terminal behavior](TERMINAL_PARITY.md), and [viewer/editor notes](VIEWER_EDITOR_PARITY.md). The detailed parity audits and [development history](docs/DEVELOPMENT_HISTORY.md) record implementation work over time; some older entries describe previous versions.

## Repository layout

- `cmd/farshmac/` — application entry point.
- `internal/ui/` — file panels, menus, dialogs, viewer, and editor.
- `internal/terminal/`, `internal/native/` — PTY sessions, scrollback storage, Cocoa bridge, and bundled renderer.
- `internal/fsops/`, `internal/remotefs/` — local/archive operations and network filesystems.
- `internal/highlight/`, `internal/preview/`, `internal/sysinfo/` — isolated highlighting/preview helpers and macOS information.
- `resources/` — application icon and upstream attribution.
- `scripts/` — checks, building, and release packaging.

## License

Our code is licensed under [MIT](LICENSE). Third-party components retain their respective licenses; see [THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt). The original Far Manager icon is distributed with its upstream BSD-3-Clause notice and [provenance](resources/upstream/README.md).
