# Changelog

## 0.19.1 — First public preview

- Publish the project as FarshMac, inspired by Far Manager, with documented installation, contribution, and security-reporting workflows.
- Add signed GitHub Actions builds and tag-driven releases using a persistent certificate stored in environment secrets.

- Add Cmd+Option+F3 (Ctrl+Alt+F3 alias) to calculate all displayed folder sizes without changing selection.
- Retain sizes across navigation and refresh in a bounded per-panel memory cache. Mark reused values with `~`; preserve size sorting and completed results after cancellation.
- Add one-time self-signed local certificate setup and automatic reuse for stable macOS signing without a paid Apple account.

## 0.19.0

- Rename the application, executable, Go module, native bridge, build artifacts, and CI configuration to FarshMac.
- Translate all project documentation to English, retaining historical version notes and limitations.
- Read previous Far Go settings, connection profiles, and window sessions when no corresponding FarshMac configuration exists; preserve the original files.
- Use the new `local.farshmac.app` bundle identifier and `FARSHMAC_` environment-variable prefix. macOS privacy permissions may need to be granted again.

## 0.18.7

- Automatically restart the parent terminal shell after exit, EOF (Ctrl+D), or a crash. Preserve terminal visibility, output history and the last known working directory; use a surviving ancestor if the directory was deleted.
- Keep nested shell exits normal, suppress restarts during application shutdown, and rate-limit retries when shell startup fails.

## 0.18.6

- Keep copy/move/delete operation failures involving remote panels visible in an OK dialog instead of only a transient status line. Do the same for remote F7 folder creation; cancellation remains quiet.
- Include source/destination paths in file-transfer failures and the target path in remote mkdir failures.
- Test F5 uploads of files and folders to SFTP in both panel directions, metadata confirmation, overwrite, and denied writes for F5/F7. The reported server-specific upload failure is not yet diagnosed.

## 0.18.5

- Returning to a parent directory focuses the folder just exited, even if the parent has a saved cursor on a different entry. Keep the parent's saved viewport and marked files. Cover synchronous and asynchronous loading.

## 0.18.4

- Fix native key-event JSON: modifier flags must be booleans, not numeric 0/1. Numeric Shift caused Go to reject the entire Option fast-find event. Correct the same issue in native keypad/selection shortcuts.
- Test native serialized key events through the Go panel handler, in addition to native modifier-type assertions.

## 0.18.3

- Route panel Option shortcuts before AppKit/WebKit handling; retain the current layout and Shift for fast find, including `.`, `_` and `-`.
- Give printable fast-find input precedence over selection punctuation while preserving explicit numeric-keypad selection shortcuts.
- Add native keyboard translation checks and panel input regression tests.

## 0.18.2

- Cmd+Shift+H now opens the interactive shell's command history (up to 500 entries), including commands entered directly in bash, sh and zsh. Select a command, edit it with F4, and press Enter to run; Shift+Enter inserts a newline and Esc cancels.
- Keep terminal output scrollback available separately under Window → Terminal Output History.
- Preserve unfinished shell input when browsing history, and retain previous Bash history entries when executing panel commands.

## 0.18.1

- Retry permanent directory removal when concurrent changes (including recreated Finder metadata) leave a directory nonempty. Retries are bounded and cancellable, and do not follow a replacement directory.
- Translate Option/Alt panel shortcuts using the current keyboard layout without Option transformations; retain the first search character even without a match.
- Calculate up to four local/archive folder roots concurrently with aggregate progress instead of retaining every file path. Remote sessions remain sequential.
- In a warm-cache local benchmark with 4096 files in 16 folders: 18.2 ms → 5.7 ms. Actual performance depends on storage and folder structure.

## 0.18.0

- Synchronize the active file panel after changing directory in the shell, including quick Cmd+O switching before the next prompt event.
- Calculate selected/current folders with F3 or all displayed folders through the Files menu. Store sizes in panels and use them in size sorting; support local, archive, and network panels.
- Preserve completed directory-size results when an operation is cancelled.
- Add a Far-style information panel with macOS disk, memory, swap, power, and folder-description data.
- Improve terminal-output batching and overlap history writes with rendering while keeping bounded queues.

## 0.17.2

- Synchronize the shell directory with the active local panel.
- Use a lightweight shell prompt by default; allow custom prompt hooks through Terminal settings.
- Switch between panels and terminal before waiting for output parsing.

## Earlier development

The original implementation notes and earlier changes are preserved in [Development history](docs/DEVELOPMENT_HISTORY.md). They include historical measurements and limitations that may have changed in subsequent versions.
