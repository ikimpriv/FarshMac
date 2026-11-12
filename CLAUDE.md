# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

FarshMac is a native macOS two-panel file manager (Far Manager style) written in Go with CGO. A Cocoa window hosts an offline WKWebView running xterm.js; the Go side owns all state and renders the panels as an ANSI character grid. macOS only; requires Xcode Command Line Tools, Go 1.25+, CGO, and Node.js 22+ for renderer tests. No npm install: xterm assets are vendored under `internal/native/assets/vendor/` and must not be hand-edited.

## Commands

```sh
./scripts/check.sh            # gofmt check, go mod verify, go vet, go test ./..., node renderer tests, clang panel_keys test
./scripts/check.sh --race     # same with -race; CI runs this. Use for goroutine/background-work changes
./scripts/build.sh            # build/FarshMac.app (host arch); --universal for arm64+amd64 via lipo
./scripts/package.sh          # universal app + zip + sha256 (release artifact)
go run ./cmd/farshmac -left "$HOME" -right /tmp   # dev run with explicit start folders (disables session restore)
open build/FarshMac.app
```

Single Go test: `go test ./internal/ui -run 'TestName'` (CGO is on by default on macOS; `CGO_ENABLED=1` if in doubt). Single renderer test: `node internal/native/tests/input.cjs` (the `.cjs` files are standalone assert scripts; `harness.cjs` is a helper and `grid.cjs` reads JSON from stdin, so don't run those as tests).

`check.sh` unsets `FARSHMAC_PROBE_STARTUP`, `FARSHMAC_PROBE_CUSTOM_PROMPT`, and `FARSHMAC_SFTP_CHECK`; tests gated by those are personal-environment diagnostics, not part of the suite. Tests use temp dirs and loopback FTP/SFTP servers from `internal/testnet`.

Local builds sign with the `FarshMac Local Development` keychain identity if present (create with `./scripts/setup-local-signing.sh`), otherwise ad-hoc. Releases: bump version/build number in `scripts/build.sh`, the displayed version in `internal/ui/draw.go`, README, and CHANGELOG (see `docs/PUBLISHING.md`).

## Architecture

**Process model.** `cmd/farshmac/main.go` locks the OS thread, prompts for Full Disk Access once (`native.PrepareDiskAccess`), builds a `ui.App`, then hands control to `native.Run`, which never returns until quit. The same executable re-execs itself as sandboxed helper subprocesses for image/PDF decoding (`--preview-helper`) and Chroma syntax highlighting (`--syntax-helper`), so a crash or slow decode cannot take down the UI.

**Native bridge (`internal/native`).** `window_darwin.m` creates one `WKWebView` per window and loads the embedded `assets/`. Communication is a strict two-way JSON protocol:
- JS → Go: `window.webkit.messageHandlers.farshmac.postMessage(...)` → `goMessage(id, json)` → `ui.WindowGroup.Handle` → `App.Handle`, which decodes a `ui.Message` (type: key, click, drag, mouseUp, wheel, scroll, resize, input, inputBinary, paste, copy, cut, selectAll, save, quit, terminalError, commandHistory, commandHistoryRun, historyAppend, historyRead; `close` is handled by `WindowGroup`). A few types never reach Go: `clipboard`, `pasteRequest`, `frameDone`, and `minSize` (the renderer's minimum content size for the current font, which sets the window's `contentMinSize`) are handled in `window_darwin.m`.
- Go → JS: a 30 Hz `NSTimer` calls `goPoll(id)` → `WindowGroup.Poll` → `App.Poll`, returning a `ui.Update` JSON that is passed to `window.farshmacUpdate(...)`. `Update.Grid` is the panel screen as an ANSI string (from `App.Draw().ANSI()`), `Update.Terminal` is base64 PTY output, `Mode` is `"panels"` or `"terminal"`, `CommandsError` reports a failed shell-history read for the ⌘⇧H overlay.

Every Go call from Cocoa is dispatched onto a single serial `engineQueue`; that queue is the "UI engine queue" referred to in CONTRIBUTING.md. All `App` state mutation must happen there, i.e. inside `Handle`/`Poll` or via closures pushed onto `a.jobs` (drained at the top of `Poll`). Goroutines must never touch `App` fields directly.

**UI engine (`internal/ui`).** `App` is the whole model for one window: two `Panel`s, optional `Dialog`/`Form`/`Menu`, the open `Document` (viewer/editor), the `terminal.Session`, and settings. `Draw()` paints everything into a `Grid` of cells. Modal state is layered: `PanelKeys` in `Update` is false whenever a document, form, dialog, or menu is open, or the app is `Busy`, so the renderer routes keys differently. `WindowGroup` multiplexes several `App`s (multiple windows) and coordinates a group-wide quit sequence that waits on each window's unsaved-work dialog in turn. Session state (window frames, folders) is saved via `WindowGroup.SaveSession`; settings and sessions live under `os.UserConfigDir()`.

**Background work.** Long-running operations go through `App.backgroundTask(label, fn)`: it creates an `fsops.Operation` (context + progress + cancellation), sets `Busy`, runs `fn` in a goroutine, and `pollTask` applies the returned `apply`/`cleanup` closures back on the engine queue. Operations needing user decisions mid-flight (overwrite/skip/retry) post questions through `a.jobs` and block the worker on a channel (`operation_questions.go`). Folder sizes, tree loading, panel loads, info panel, previews, and highlighting all follow the same poll-and-apply pattern and must stay cancellable and memory-bounded.

**Filesystem layers.**
- `internal/fsops`: local copy/move/delete/trash with macOS metadata (clonefile, xattrs, Finder flags), collision/recovery policy (`docs/FILE_OPERATIONS.md`), parallel copy, archive reading (ZIP/TAR/compressed TAR treated as read-only directories).
- `internal/remotefs`: `FS` interface with FTP/FTPS/SFTP implementations plus a local adapter; host-key verification uses `~/.ssh` defaults and must not be weakened silently.
- `internal/ui/archive_panel.go`, `remote.go`: adapt those backends into panels.

**Terminal (`internal/terminal`).** `Session` wraps a PTY running bash/sh/zsh (with `TERM_PROGRAM=FarshMac`; Terminal.app session variables are stripped, so tests such as `TestSelectableShells` do not depend on the host terminal) with an injected prompt-hook protocol (`bourne.go`) that reports cwd, exit status, and readiness as `Event`s. Directory sync between panels and shell only happens when the shell is at a prompt; never send integration keys while a program is running. Scrollback is spooled to temp files with a bounded in-memory window (`history.go`).

**Renderer (`internal/native/assets`).** `app.js` owns two xterm instances (panel grid and real terminal), key translation (macOS Option/dead keys, Fn keys), mouse → `Message` mapping, and history views. Node tests in `internal/native/tests` load these scripts in a `vm` context with a fake DOM/xterm (`harness.cjs`) and assert on the messages sent. `panel_keys.m` is a compiled Cocoa test for Option-key translation.

## Conventions that matter here

- Keep documented shortcuts in sync across the menus (`menu.go`), the F1 `help` string in `app.go`, and README.
- Behavior-parity documents (`BEHAVIOR_PARITY.md`, `MENU_PARITY.md`, `TERMINAL_PARITY.md`, `VIEWER_EDITOR_PARITY.md`, `ARCHIVE_SUPPORT.md`) record Far Manager compatibility decisions; consult them before changing key handling or dialogs.
- Never commit credentials, private paths, connection profiles, or user settings; test fixtures are synthetic.
- When replacing vendored assets, record the upstream version and update `THIRD_PARTY_NOTICES.txt`.
- `.editorconfig`: 2-space indent everywhere except Go (tabs); LF line endings.
