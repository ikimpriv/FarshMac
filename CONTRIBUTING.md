# Contributing

FarshMac currently targets macOS. Development requires Xcode Command Line Tools, Go 1.25+, and Node.js 22+ for renderer checks. No npm installation is needed: the xterm assets are vendored.

1. Create a branch for a focused change.
2. Implement it and add meaningful regression tests for behavior changes.
3. Run `./scripts/check.sh`; use `./scripts/check.sh --race` for changes involving background work or state shared across goroutines.
4. Run `./scripts/build.sh` and verify relevant GUI interactions.
5. Open a pull request describing the problem, new behavior, and validation.

## Implementation conventions

- Format Go with `gofmt`; keep UI state changes on the UI engine queue.
- Keep filesystem scans, network operations, preview decoding, and expensive highlighting off the UI thread.
- Long-running work must support cancellation and bounded memory use.
- Preserve file-operation safety and metadata behavior; avoid silently weakening host-key verification or ignoring copy errors.
- Use synthetic fixtures and loopback servers in tests. Never commit credentials, private paths, connection profiles, or user settings.
- Keep documented shortcuts consistent with the menus and F1 reference.
- Do not edit minified vendor assets by hand. Record the upstream version and update licenses when replacing them.

`grid.cjs` is a stdin-driven rendering helper, and `harness.cjs` is a test helper. Use `scripts/check.sh` instead of running every `.cjs` file indiscriminately.

Tests gated by `FARSHMAC_PROBE_STARTUP` or `FARSHMAC_SFTP_CHECK` are explicit personal-environment diagnostics. They are not part of CI; the standard check script clears these variables.

## Licensing

Contributions to the project's own code are accepted under the MIT license. Preserve upstream copyright and license notices, and add attribution for any new dependency or borrowed material. There is no contributor license agreement.
