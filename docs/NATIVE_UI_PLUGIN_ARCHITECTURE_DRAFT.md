# Native UI and JavaScript Plugin Architecture

**Status:** Draft for discussion; not an implementation commitment  
**Date:** 2026-11-14  
**Scope:** macOS, Linux, Windows; Classic and Modern presentations; JavaScript plugins with graphics  
**Implementation status:** Proposed. Existing FarshMac remains the reference for supported behavior.

## 1. Intent and boundaries

Build one file manager with shared behavior and two presentations on each supported desktop OS:

- **Classic:** character-cell layout, Far-style frames, menus, dialogs, and keyboard workflows.
- **Modern:** graphical file lists, icons, flexible columns, contextual controls, and graphical previews.

Both presentations use the same operations, selections, documents, connections, bookmarks, and terminal sessions. Modern must retain efficient keyboard operation. Classic may embed real graphics; it is not restricted to terminal escape sequences.

The target product must not require users to install Go, Node.js, Python, a browser, or a package manager. Build-time dependencies and libraries shipped inside the application are allowed, subject to licensing and packaging review.

Native means native windows, platform input and accessibility integration, and direct platform rendering without a required WebKit/Chromium runtime. It does not mean every control must be a stock OS widget, nor does it guarantee performance by itself.

Out of scope for the initial migration:

- Compatibility with Far/far2l binary plugins or their macro languages.
- Universal support for arbitrary npm packages, DOM, or browser Canvas.
- A terminal-only application runnable over SSH. Preserve architectural room for it, but do not include it implicitly in desktop parity.
- Identical filesystem semantics or preview formats across operating systems.
- Rewriting the editor model or terminal emulator merely to change appearance.

## 2. Current implementation and migration constraints

Today Cocoa hosts WKWebView. Go owns application state and generates a cell grid, serializes it to ANSI, and sends JSON updates to JavaScript. Two xterm.js instances render the panels and the PTY terminal. Images are decoded in helper processes and transferred as encoded image data for an HTML image overlay.

Existing assets to retain include file-operation policy, connection implementations, document models, terminal session management, cancellation, and regression tests. Existing UI state is partly coupled to grid coordinates and a single active operation; separating these concerns is real implementation work, not a renderer substitution.

The current renderer stays operational during migration. Do not mark native platforms or presentations supported merely because they compile.

## 3. Logical architecture

```text
Platform window / input / accessibility
                  |
         Presentation controller
          Classic       Modern
                  |
       Commands + semantic view state
                  |
              Go application core
   Panels / documents / operations / connections
          |                         |
   Platform services           Plugin broker
          |                         |
 Filesystem / PTY / preview     Isolated plugin hosts
                                     |
                             Embedded JS runtimes
```

### 3.1 Core and semantic state

The core exposes stable entity IDs, commands, snapshots, and revisioned updates. It must not expose AppKit objects, DOM objects, platform pointers, ANSI strings, or cell coordinates as its primary public interface.

Example state includes panel location, paged entries, cursor ID, marked IDs, sort/filter settings, document revisions, operation progress, pending decisions, and available actions. Paths alone are not stable identities for virtual or remote items.

Commands express intent, such as `CopySelection`, `OpenDocument`, or `ResolveConflict`. Platform-specific key chords map to commands outside the domain model. User bindings use semantic command IDs; defaults remain platform-aware.

A presentation-independent reducer validates commands and owns mutable UI/application state. Workers return immutable results through that owner. Native UI calls stay on the platform's required UI thread; filesystem, network, decoding, and plugin execution do not run there.

Updates carry monotonically increasing revisions and entity generations. Consumers discard stale results. Large listings are paged and virtualized; never serialize an entire large directory on every cursor movement.

### 3.2 Presentation adapters

Classic maps semantic state to cell layout. Modern maps it to graphical layout. Neither invokes filesystem mutation directly or implements independent conflict policies.

Dialogs are semantic requests: fields, validation, available choices, explanatory details, and a completion token. The adapter owns visual layout and focus. Security-sensitive or destructive dialogs remain host-controlled, including when a plugin initiates the operation.

Switching Classic/Modern is per window. Preserve locations, selection, open documents, operation IDs, shell sessions, and bookmarks. Preserve scroll anchors by item/document position rather than pixel offset. Cancel an unfinished IME composition deliberately; never commit it silently into another view. If a modal interaction cannot be migrated safely, defer the switch until it closes.

Settings record the preferred presentation, not separate copies of application state.

## 4. Platform rendering strategy

The following are candidates to validate with prototypes, not frozen backend choices:

| Platform | Windowing and input | Text/graphics direction | Native integration |
|---|---|---|---|
| macOS | AppKit | Core Text + Core Graphics initially; Metal only if profiling justifies it | Pasteboard, accessibility, Quick Look, Keychain, system file services |
| Windows | Win32; evaluate framework needs for Modern | DirectWrite + Direct2D; GPU-backed surfaces where useful | UI Automation, shell actions, credential storage, ConPTY |
| Linux | Select one supported toolkit first, with Wayland and X11 coverage | Toolkit text/graphics stack, e.g. GTK/Pango/Cairo or its GPU renderer | Accessibility, clipboard, desktop portals and secret services where available |

Linux has no single system-native desktop toolkit. The toolkit, minimum versions, distribution dependencies, and packaging policy are unresolved decisions. Ship required non-system runtime components where practical; document the actual minimum desktop baseline. Do not assume an installed utility or service merely because it is common on developer machines.

Use a shared retained scene description for custom content: text runs, filled/stroked shapes, images, clips, transforms, and hit-test regions. Standard Modern controls may use native widgets without reproducing them as scene primitives. Keep this boundary explicit to avoid maintaining two competing layout systems for every control.

Render dirty regions, cache shaped text with bounded budgets, and respect display scale and font changes. Do not create one native widget per character or file entry. Start with native 2D APIs; GPU-only rendering is not a prerequisite.

## 5. Text, input, accessibility, and terminal

### Text and input

Separate grapheme boundaries, terminal cell width, glyph shaping, and pixel geometry. Handle combining marks, wide characters, emoji, fallback fonts, bidirectional text policy, and ligatures explicitly. Do not use byte offsets as cursor positions.

Implement platform text-input protocols, IME composition, dead keys, keyboard layouts, clipboard formats, focus traversal, and selection. Native rendering alone does not provide these features automatically.

Expose semantic controls to platform accessibility APIs. Classic also needs accessible file entries, dialog fields, editor content, and focus; a single opaque drawing canvas is insufficient.

### Terminal

Keep shell transport separate from terminal emulation and rendering:

1. Platform transport: Unix PTY on macOS/Linux, ConPTY on Windows.
2. Emulator: screen state, escape parsing, modes, cursor, alternate screen, mouse protocols.
3. Renderer: native cell surface and selection.
4. History: bounded memory, disk spool, lifecycle cleanup.

Evaluate an existing embeddable emulator before designing a new one. Selection criteria include license, Unicode behavior, VT compatibility, resize/reflow, throughput, maintained APIs, and fuzz testing. The engine is not selected by this document.

During the first migration stages, xterm.js/WebKit may remain exclusively for terminal display. This is transitional and limits memory savings; full native acceptance requires removing that dependency from normal operation.

Use compatibility tests with interactive applications and synthetic escape streams. Preserve shell restart, command history, working-directory synchronization, and interrupt behavior. Sharing a renderer with Classic does not make the file panels a terminal application.

## 6. Platform services and capabilities

Define explicit service interfaces for filesystem metadata, atomic replacement, links, Trash, system opening, credentials, previews, notifications, and shell transport.

Return capability information and structured errors. Never silently promise POSIX ownership on Windows, atomic replacement on every remote server, or lossless metadata transfer between unrelated filesystems. Existing retry/skip/cancel and metadata-loss decisions remain authoritative.

Quick Look is a macOS preview provider, not the universal preview API. Other platforms may use different system providers or bundled decoders. Installed third-party handlers are optional capabilities. Previewing remote/archive content requires a cancellable materialization step with bounded temporary storage and cleanup.

All paths and resource handles must distinguish local, remote, archive, and plugin-owned namespaces. Avoid exposing native file handles to untrusted extensions.

## 7. JavaScript plugins

### 7.1 Execution model

Default to one host process per active plugin, launched from an application-supplied executable mode. Each host embeds a JS engine and communicates with the broker. No external Node.js installation is required. Hosts start lazily and may exit when idle.

Goja is an initial candidate, not a commitment. Prototype supported language syntax, async behavior, interruption, throughput, and memory control before selection. Define a versioned SDK language baseline; TypeScript is compiled by plugin authors into distributable JavaScript.

Do not promise Node.js, browser APIs, arbitrary native npm modules, or general ESM compatibility without implementing and testing them. Pure-JS dependencies may be bundled by authors under the documented runtime contract.

A JS runtime has a single execution owner/event loop. I/O runs asynchronously in the host/broker and completes back on that owner. Never execute plugin callbacks on the application UI thread.

### 7.2 Packages and lifecycle

A plugin package contains a manifest, bundled JS, optional assets, licenses, and optional platform-specific helpers. The manifest declares ID, version, SDK range, entry point, contributions, required permissions, and supported platforms.

Lifecycle: discover -> validate -> authorize capabilities -> start -> activate -> contribute -> deactivate -> stop. Removal unregisters commands and views, cancels work, and releases subscriptions and resources. Updates stage separately and activate after existing work drains; keep rollback metadata.

Default plugin capabilities include commands, event subscriptions, declarative forms, virtual panels/filesystems, text transforms, metadata providers, and graphical views. Plugin widgets never mutate core state directly.

### 7.3 Protocol and bulk data

Use a versioned, length-framed local RPC protocol over private pipes or equivalent private IPC. Messages include request IDs, cancellation tokens, deadlines, and structured errors. Negotiate SDK/protocol compatibility before activation.

Control messages may use JSON. File content, images, and large numeric arrays use bounded binary streams or scoped resource handles. Apply backpressure, maximum message sizes, outstanding-request limits, and paging. Do not base64-encode bulk data by default.

Late responses after cancellation or view disposal are ignored using generation IDs. Events are batched or coalesced where semantics allow; destructive decisions and operation completions must not be dropped.

### 7.4 Permissions and failure isolation

An isolated process is not automatically a security sandbox. Enforce platform-specific restrictions on the host where supported, and expose brokered capabilities with narrowly scoped file/network/process access. If an intended restriction cannot be enforced on a platform, disclose that fact and do not label the execution sandboxed.

No ambient filesystem, network, credential, or process access in the JS API. Permissions are declared and granted explicitly. Credential handles remain opaque; do not provide broad keychain access.

Apply deadlines, cooperative cancellation, and forced host termination for nonresponsive plugins. Memory and CPU budgets require platform enforcement or supervision; a Go runtime memory target is not a hard process limit. Killing a plugin must release handles and mark affected operations failed, without discarding unrelated documents or rolling back already committed file operations silently.

Prevent plugin-generated UI from impersonating host permission prompts. Escape labels and validate resource references. Avoid logging credentials and file contents by default.

## 8. Plugin graphics

Provide three levels, using the same API in Classic and Modern:

1. **Declarative components:** charts, tables, trees, progress indicators, images.
2. **Retained custom scenes:** paths, shapes, text, transforms, clipping, hit regions.
3. **Raster resources:** bounded decoded images or encoded resources decoded by controlled helpers.

Charts support named series, axes, legends, hover, zoom, and selection. Incremental updates append or replace bounded data ranges. Define decimation for large series and retain source data outside the renderer when necessary.

Illustrative API only:

```js
const chart = await far.views.createChart({
  title: "Transfer speed",
  series: [{ id: "speed", kind: "line" }],
  axes: { x: { unit: "s" }, y: { unit: "MiB/s" } }
});
await chart.append("speed", [{ x: 1, y: 120 }, { x: 2, y: 145 }]);
```

The application owns redraw scheduling. A plugin submits batched scene changes, never one RPC per primitive per frame. Validate scene size, coordinate ranges, nesting, text lengths, image dimensions, and update rate. No raw GPU pointers, shaders, or native view handles in the initial API.

Input events reference stable scene element IDs and logical coordinates. Route focus explicitly; global application shortcuts remain available. Every custom interactive view supplies accessible labels/actions and a text or table fallback. A future terminal-only frontend can use that fallback; desktop Classic can display full graphics.

Browser chart libraries are not directly compatible. A web-panel extension could be explored later, but is excluded from the default native architecture and would reintroduce web-runtime costs.

## 9. Performance and resource acceptance

Performance improvements are hypotheses until measured. Capture a reproducible baseline using the same release, data, fonts, window dimensions, display scale, and history settings. Record all application/helper/renderer processes rather than comparing only parent-process RSS.

Measure cold/warm startup, idle CPU, total private memory, cursor-to-frame latency, resize latency, directory paging, terminal throughput, image decode latency, and plugin update overhead. Separate producer-write completion from visible-frame completion. Report distributions and peaks, not a single best result.

Provisional prototype targets, to validate on named reference hardware:

- No filesystem/network/plugin I/O on the UI thread.
- Interactive cursor updates reach the screen within 50 ms at p95 under ordinary load.
- Drawing budget fits one refresh interval for ordinary navigation at the tested refresh rate.
- Memory reaches a bounded plateau during sustained terminal output, repeated preview navigation, and live charts.
- Plugin floods do not starve input or core operation completion.
- No regressions in copy correctness, cancellation, unsaved-document protection, or keyboard behavior.

Set concrete memory, cache, plugin, and payload budgets after baseline measurements. Do not advertise a fixed percentage improvement before testing. Native rendering must pass correctness gates even when it is faster.

## 10. Migration and validation gates

| Stage | Deliverable | Exit gate |
|---|---|---|
| 0 | Baseline and semantic command/state contracts | Existing behavior captured in tests; benchmarks reproducible |
| 1 | Adapt current WebKit UI to semantic state | No behavior regression; domain state no longer depends on ANSI transport |
| 2 | Native macOS Classic prototype | Panels, dialogs, editor input, resize, images and accessibility validated; terminal may remain transitional |
| 3 | Modern macOS panels | Shared commands/dialog policy; live presentation switching preserves state |
| 4 | JS host and small SDK | Example command, virtual panel and chart; cancellation/crash/permission tests pass |
| 5 | Native terminal replacement | VT/Unicode/input/history compatibility and throughput gates pass |
| 6 | Windows and Linux vertical slices | Local browsing, copy, editor, shell and plugin chart on each platform |
| 7 | Platform parity and distribution | Signed/packaged artifacts, clean-machine installs, documented capability differences |

Validate at least one non-macOS vertical slice before freezing plugin APIs or platform abstractions. Stages may overlap, but shipping claims follow completed gates.

Test at four levels: core unit/integration tests, semantic presentation contracts, platform UI/IME/accessibility tests, and packaged clean-machine tests. Include malicious/buggy plugin fixtures, malformed protocol messages, renderer resource exhaustion, permissions revoked mid-operation, and preview cancellation during rapid navigation.

## 11. Open decisions and risks

- Linux toolkit, supported desktop environments, minimum versions, and distribution packaging.
- JS engine and language baseline; plugin host memory budget and enforced sandbox policy per OS.
- Terminal engine, license, and adaptation cost.
- Which Modern controls use stock widgets versus the common custom scene renderer.
- Plugin distribution, signing/trust, update source policy, and SDK compatibility window.
- Shared graphics semantics for text, fonts, accessibility, and export across platforms.
- Linux/Windows replacements for macOS-only preview and metadata facilities.

Principal risks are duplicated presentation policy, incomplete Unicode/input behavior, premature public SDK commitments, overpromised plugin isolation, and measuring renderer speed while ignoring helpers or asynchronous work.

## 12. References and status discipline

- Current implementation: `internal/ui`, `internal/native`, `internal/terminal`, `internal/preview`, `internal/fsops`, `internal/remotefs`.
- Current behavior: [Functional specification](FUNCTIONAL_SPEC.md), [file operations](FILE_OPERATIONS.md), [viewer/editor parity](../VIEWER_EDITOR_PARITY.md), [terminal parity](../TERMINAL_PARITY.md).
- [Core Text](https://developer.apple.com/documentation/coretext), [AppKit text input](https://developer.apple.com/documentation/appkit/nstextinputclient), [Quick Look UI](https://developer.apple.com/documentation/quicklookui).
- [Goja](https://github.com/dop251/goja) and [optional Node compatibility modules](https://github.com/dop251/goja_nodejs).

This draft must remain separate from documentation of shipped functionality. API examples are proposals. Selecting this architecture does not mean native Linux/Windows ports, Modern UI, or JS plugins are already implemented.
