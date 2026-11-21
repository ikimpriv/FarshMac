# AI Spaces Architecture

**Status:** Draft for discussion; not an implementation commitment  
**Date:** 2026-11-20  
**Scope:** Virtual project environments, overlay filesystems, isolated console agents, MCP, messaging, and FarshMac panels  
**Implementation status:** Proposed. A proof of concept exists (see “Proof of concept” below); it is not the proposed security profile and nothing here is shipped.

## Proof of concept

`internal/aispaces` and `internal/ui/ai_spaces.go` implement enough of this draft to try the workflow. It exists to evaluate the interaction model, not the security model.

| Draft area | PoC |
| --- | --- |
| Catalog (§3) | JSON file under the application config folder, `ai-spaces/catalog.json`; no SQLite, no artifacts, messages or tasks |
| Panels (§9) | Left/Right → **AI Spaces**; Spaces and Envs as folders; F7 creates a Space / an Env with agent checkboxes / an overlay folder; Shift+F6 renames a Space or Env; F8 deletes a Space, an Env, an agent, or hides an entry |
| Source registration (§4.1) | F5 from a local panel attaches references without copying and asks **Read/write** or **Read-only**; F6 into an Env is refused |
| Filesystem (§4.2) | **Live references** (decision of 2026-11-20, see §4): reads and writes below a referenced folder or file go straight to the real path, so every Env that references it sees the change. Removing an entry inside a reference only hides it from this Env and its agents (the real file is not deleted; F11 → **Show removed entries again** undoes it); removing the reference itself detaches it. Paths outside references live in a private folder per Env. No overlay, whiteouts or revert |
| MCP (§7) | One private Unix socket, a stdio gateway (`farshmac --ai-mcp-gateway`), authority bound to a launch token; tools `vfs_list/stat/read/write/edit/mkdir/remove/rename/search` and `agent_peers/agent_send/agent_reply`. After reviewing the first logs the tools were reshaped to need fewer calls: `vfs_list` takes a depth and marks reference roots with their ID and read-only state, `vfs_read` returns a line range, `vfs_edit` applies a batch of replacements across files or nothing, `vfs_search` takes a regular expression, a name filter and context lines. **References:** every attached real path has one ID per Space; `@id/path` names the same real file in every Env whatever virtual path it was attached at, every tool accepts it in place of a path, and `vfs_stat` reports it for quoting to other agents. An agent cannot detach or rename a reference. Every message is logged per Env in `mcp-log.jsonl` (F11 → **MCP debug log**, on an Env, inside it, or on an agent) with arguments, result head, sizes, server time and the gap since the previous call |
| Messaging (§8) | Agents are addressed as `Env/agent`. `agent_send` returns at once; the message is typed into the recipient's console as a prompt, and its `agent_reply` is typed into the asker's console the same way, so nobody polls. A recipient that is not running is started for the message, without a window (decision of 2026-11-20, replacing “delivery does not start a dormant agent” in §8); its output is buffered and its console can be opened later. The folder trust question of a new scratch folder is answered automatically. F11 → **Communication…** sets which Envs the agents of an Env may contact; an agent can always answer, and ask its sender back while the request is open. F11 → **Space messages** shows the log. Verified with two Claude Code agents. A reply can be answered in the same thread. When a message names a reference its recipient cannot open, the sender is warned and the user is told which folder to attach. No tasks or artifacts, and no budget or loop limit on agents starting each other |
| Operator MCP (not in the draft) | A second stdio server, `farshmac --ai-mcp-operator`, for an agent the user runs outside any Env (Codex, Claude Code). It speaks for the user and is not bound by Env communication rules. Tools: `spaces_list`, `agent_status` (state, unread replies, tail of the agent's own transcript, optional console view), `agent_send` (starts a stopped agent), `agent_wait` (blocks until a reply or until the console falls silent). Agents answer with the ordinary `agent_reply`; replies wait in the catalog until read. The gateway answers the protocol itself and finds the running FarshMac through `ai-spaces/operator.json` (socket and token, mode 0600, unreadable inside the agent sandbox), so it survives FarshMac restarts. Calls are logged in `ai-spaces/operator-log.jsonl`. It has no file tools. Idle detection is a heuristic (console quiet for 3 s); the transcript view is Claude-only. |
| Agent instructions | Each agent has its own file in the Env root, named as its CLI expects (`CLAUDE.md` for Claude Code, `AGENTS.md` for Codex). The user edits it there; at each start it is combined with the FarshMac notes and placed in the agent's working folder |
| Agents (§6, §9) | Enter on an agent executable starts or reveals its retained console; Cmd+O switches between panels and the Env's last console; the agent keeps running while hidden. Claude Code is verified end to end; the Codex adapter is written but untested |
| Session continuity (§6.4) | Claude gets an assigned session ID and is resumed with it after it exits or FarshMac restarts |
| Isolation (§6.2) | Each agent runs under a macOS sandbox profile (`sandbox-exec`, the “restricted host process” profile). It can write only to its own scratch folder and cannot read the user's home folder, other volumes, other agents' scratch, other projects' CLI sessions or the shared prompt history. Project files reach it only through the Env's MCP server, which keeps the context of each Env separate. Allowed besides scratch: the CLI's own configuration and session folder, its installation, the login keychain, its own temp folder. Not restricted: network, and reading outside the home folder. The launch fails closed if the sandbox is unavailable. Verified with Claude Code; `sandbox-exec` is deprecated by Apple, so this is a PoC mechanism, not the final one |
| Workspace (§5) | `workspace_pull` copies a project folder into the agent's scratch so its own tools can build and test; `workspace_push` copies it back and refuses files changed in the project since the pull. `vfs_copy` copies inside the Env. The server moves the bytes, so content is not generated twice |

Not in the PoC: revisions and conflict detection between concurrent writers, materialization, quotas, symlink containment, remote sources, stable-ID bookmarks, status markers in the file list, and input ownership across windows (an agent running in one window cannot be opened from another).

## 1. Intent

An **AI Space** is a project containing multiple **AI Envs**. Each Env has a responsibility, an independently controlled view of project data, a private writable overlay, scratch storage, and one or more independently retained agent consoles. Initial agent adapters target Claude and Codex; their actual CLI/configuration compatibility must be verified during implementation.

Users provision an Env by copying files or directories into its panel. This registers virtual references, not a recursive physical copy. An agent works with the resulting filesystem through MCP. References are live: changes below them are saved to the real folders and are visible in every Env that references them (see the decision note in §4).

A typical Space contains Product Manager, Architect, Developer, and Acceptance Tester Envs. These are role presets, not security principals with fixed privileges. Users may create several developers or omit roles. Messages coordinate responsibilities; artifact grants control which outputs another Env can actually read.

### Required invariants

1. Adding a source does not duplicate its tree or grant access to its parent directory.
2. A referenced folder or file is live: what an agent or the user writes below it in an Env is saved to the real path, and every Env that references it sees the change. Paths outside references are private to the Env. Removing a reference never deletes its source.
3. Agent authority comes from its authenticated Env/session binding, never an Env ID supplied in a tool argument.
4. One Env cannot read another Env's data, messages, scratch, or credentials without an explicit grant.
5. A virtual pathname is never passed to a local shell as a host pathname.
6. Cancellation stops further work and releases streams; committed changes remain identifiable and recoverable.
7. Attaching a reference is the user's grant of write access to that folder or file; there is no separate publication step.
8. MCP access controls do not substitute for process isolation.

### Initial non-goals

- A kernel-mounted FUSE filesystem or transparent POSIX mount.
- Automatic access to all files visible to FarshMac, remote connections, Docker sockets, or Kubernetes credentials.
- Arbitrary third-party MCP servers with the same authority as the trusted filesystem broker.
- Fully autonomous role orchestration, distributed Space hosting, or concurrent writers inside one Env.
- Claiming macOS, Linux, and Windows isolation parity before platform validation.

## 2. Relationship to the current application

FarshMac currently routes panels through `internal/ui/panel_backend.go`, documents through `documentSource`, traversal through browsing/search strategies, and retained consoles through `internal/shellsession`. These are integration points, not permission boundaries.

The proposed AI Spaces domain lives outside `internal/ui`. Panels and MCP call the same service for filesystem operations, revision checks, progress, and cancellation. Existing background tasks and operation questions present work on the UI engine queue. Workers must not mutate `App` state directly.

The existing application can have broad host access, including user-granted Full Disk Access. Agent processes must not inherit equivalent authority. The trusted broker can read registered sources; the agent receives only checked operations on its virtual namespace.

```text
FarshMac panels / viewer / editor / operation dialogs
                         |
                 AI Spaces service
          Catalog + policy + operation coordinator
             /             |              \
      Overlay store   Message/artifact   Session supervisor
             |            broker               |
       Source adapters                   Runtime isolation
             |                          Agent + private scratch
     Approved host roots                       |
             ^                                 |
             +---- Env-scoped MCP gateway <----+
```

Suggested packages (provisional):

| Package | Responsibility |
| --- | --- |
| `internal/aispaces` | Space/Env lifecycle, identities, policies, operation coordination |
| `internal/aivfs` | References, overlays, revisions, conflicts, traversal, materialization |
| `internal/aimcp` | MCP adapter, authenticated Env scope, bounded tools/resources |
| `internal/aiagents` | Agent adapters, launch specifications, runtime adapters, session supervision |
| `internal/aimessages` | Durable messages, tasks, acknowledgments, artifact grants |
| `internal/ui` | Panel backend and presentation of domain commands/events |

No package name or API in this draft is a public compatibility promise.

## 3. Domain model and persistence

| Entity | Essential fields |
| --- | --- |
| Space | Stable ID, name, schema version, Env IDs, sharing policy, retention policy |
| Env | Stable ID, Space ID, role instructions, source bindings, overlay revision, runtime policy, quotas |
| SourceBinding | ID, virtual destination, source-provider handle, granted root identity, live/snapshot policy, exclusions, grant generation |
| OverlayEntry | Stable ID, virtual path, kind, content blob/ref, base fingerprint, revision, provenance |
| AgentEntry | Stable ID, Env ID, selected adapter, executable name, detected installation/runtime binding, launch settings |
| AgentSession | ID, AgentEntry ID, Env ID, adapter/version, runtime ID, lifecycle state, console ID, authority generation, adapter-native session reference for resume |
| Artifact | Immutable manifest/blob references, producing Env and revision, recipient grants, expiry |
| Message | ID, Space ID, authenticated sender, recipient(s), thread/reply IDs, body, artifact IDs, timestamps |
| Task | ID, title, assignee Env, criteria, dependency IDs, revision, status, evidence artifact IDs |
| Operation | ID, owner/session, scope, progress, cancellation state, idempotency key, completion journal |

Use a transactional catalog (SQLite is the initial candidate) and immutable content blobs. Large file content does not live in catalog rows or JSON settings. An implementation decision must select and license the SQLite driver and validate locking against a second running application instance (windows of one instance share a process).

Private application storage is partitioned by Space and Env. Scratch is a separate real directory or runtime volume, not a path inside the authoritative overlay database. It survives switching panels; cleanup is explicit or follows a visible retention policy. Agent processes never receive access to the catalog directory or blob store itself.

Store host source handles and mappings privately. Exports omit absolute host paths, credentials, and live session tokens by default. Persist schema versions, migration backups, and crash-recovery state. Reference deletion and blob garbage collection are transactional; referenced artifacts and open operations pin blobs.

## 4. Virtual filesystem semantics

> **Decision (2026-11-20): references are live and writable.** After trying the PoC, the copy-on-write overlay was dropped. Writes below a reference go to the real folder or file immediately and are visible to every Env that references it; only paths outside references are private to an Env. This supersedes the overlay, whiteout, revert and conflict rules in §4.2–§4.3, the separate publication step in §5, and the “changes stay in the Env” wording elsewhere. Those sections still describe the earlier model and need rewriting. Open consequences: concurrent writers in different Envs are not coordinated, an agent can now change real project files (it cannot delete them: removal only hides an entry from its Env), and isolation (§6) matters more because a reference is now a write grant.

### 4.1 Namespace and source registration

An Env exposes logical paths such as `/project/backend`, `/requirements`, and `/results`. These are not host directories. Space/Env catalogs contain virtual folders. Each configured agent appears inside its Env as a virtual executable entry. This is a typed, application-owned launch handle, not a host executable or script. Tasks and messages remain actions or separate views.

F5 from a local panel to an Env creates SourceBindings at the requested virtual destination. Register directories lazily: do not enumerate all descendants just to attach a large source. A file binding grants exactly that file. Name collisions use the normal overwrite/rename/skip vocabulary, but replacing a binding never deletes its source.

The initial source adapter supports local regular files and directories. Remote/container sources can be added later through the same interface with explicit credentials, latency limits, reconnect behavior, and stable identity. Do not silently import all authority of an existing remote connection.

Nested mounts are traversal boundaries by default. Following a symlink or entering a mount outside the granted root requires another grant. A symlink is displayed as a link; merely importing it does not authorize its target. Hard links must not expose alternate host paths. Special files, device nodes, sockets, and named pipes are not readable source content in the initial version.

Path resolution uses component-aware containment and race-resistant, handle-relative access where the platform permits. String-prefix checks and a single `realpath` call are insufficient. Validate paths again at use time; reject traversal, alternate path syntax, NULs, and stale/replaced roots. Virtual names are case-sensitive; reject ambiguous case/Unicode collisions when materializing or exporting to a less expressive filesystem.

### 4.2 Read and write rules

Lookup order is writable overlay, whiteouts/opaque-directory rules, then the granted source view. Each operation uses one namespace revision; paged enumeration carries a revision cursor and reports when that cursor becomes stale.

| Action | Env result | Effect on source |
| --- | --- | --- |
| Read untouched file | Read through the registered provider | None |
| Write or edit | Create an overlay blob and entry revision | None |
| Create file/directory | Add an overlay entry | None |
| Delete source-backed entry | Record a whiteout | None |
| Delete overlay entry | Remove it; retain a whiteout if it shadows a source entry | None |
| Delete directory | Hide its entire subtree with an opaque tombstone | None |
| Rename/move within Env | Atomic virtual namespace change plus source whiteout where required | None |
| Copy within Env | Independent entry backed by immutable content or an independent reference | None |
| Revert entry/subtree | Remove selected overlay changes and whiteouts | None |
| Export | Materialize the effective view to an explicit destination | Normal destination writes only |
| Apply to original | Reviewed, conflict-checked publication operation | Explicit user-approved changes |

A deleted directory hides even children subsequently added to its live source. Recreating that directory starts empty unless the user explicitly restores its source view. Renaming a source-backed directory must remain lazy, using a binding/redirect with well-defined whiteout rules rather than copying the entire tree. Prevent moving a directory into itself and detect redirect cycles.

Shift+F8 must not bypass the overlay and delete real sources. Any permanently discarded data is limited to the Env's own history/blobs after confirmation and reference checks. F6 across host/Env boundaries is initially disabled: a host move would contradict reference-import semantics. Users use F5 to attach or export, and an explicit detach action to remove a source grant.

### 4.3 Live sources, snapshots, and conflicts

**Default: live references with copy-on-write.** Untouched entries reflect source changes. Once edited, the overlay version wins. The first edit captures a baseline fingerprint and, when needed for later diff/merge, the baseline bytes. The broker reads and fingerprints from the same opened file, checking for concurrent changes before accepting a baseline.

**Optional: pinned inputs.** A Git commit or an explicit snapshot can supply a stable baseline. Snapshot creation may require copying content; show its cost and progress. Never label live references as reproducible snapshots. Lazy first-read capture stabilizes individual files, not an entire directory revision.

External changes mark affected overlay edits as conflicted; they do not silently replace agent work. Publication compares the source against its recorded baseline, including creations, deletions, metadata and renames. Display expected versus current state and offer retry/rebase/skip/cancel. Default to conflict rather than destructive overwrite.

Multiple Envs referencing one source have independent overlays. Cross-Env merging happens through immutable artifacts and explicit acceptance. Within one Env, writes use expected entry revisions; concurrent editor/agent changes fail with a conflict. Multiple configured agents may run concurrently in one Env and share its effective virtual filesystem. Serialize overlay commits transactionally and require expected entry revisions for writes, renames and deletes; reject stale mutations rather than silently overwriting another agent or editor. Agents requiring independent changesets use separate Envs. Each agent has its own identity, console, authority token and scratch subdirectory; materialization imports use baseline revisions to detect intervening edits.

### 4.4 Large trees and cancellation

Directory listing and search stream bounded pages. No recursive load on panel entry. Enforce limits on open handles, in-flight reads, response bytes, content sniffing and index size. Source revocation, selection changes, Env closure, and Esc cancel relevant work, following existing Quick Look/search behavior.

Use chunked file reads and writes, temporary staging plus atomic commit, and configurable blob/scratch quotas. Large deletions can install a directory whiteout without traversing its source tree. Background indexing is optional and revision-aware. Caches are keyed by Env, source grant generation and overlay revision, never by virtual path alone.

## 5. Materialization and publication

MCP tools do not make arbitrary compilers, Git, or shell programs understand a virtual filesystem. Provide an explicit **materialize** operation for a selected effective subtree:

1. Check the Env grant and pin a source/overlay revision manifest.
2. Copy the effective view into an Env-local scratch workspace; do not follow external links.
3. Run builds/tests using only that workspace and allowed runtime tools.
4. Import chosen outputs or edits into the overlay using the manifest as the conflict baseline.
5. Keep generated files outside the overlay unless explicitly imported.

Materialization is a real copy, unlike source registration. Show that distinction in progress and storage usage. Never use writable hard links to original files. Platform copy-on-write clones are allowed only when their write isolation is verified. This operation requires a separate capability because it exposes provided content to console tools for the lifetime of the materialization.

Publishing to original sources is unavailable to agents by default. The user reviews a changeset containing additions, edits, renames and deletions; unresolved conflicts prevent those items from being applied. Do not execute hooks or commands embedded in an artifact.

A publication journal records each completed write, expected prior version and recovery data. Multi-file publication is not assumed atomic. Cancellation leaves successful items applied and remaining items pending, with an accurate progress/result view. A rollback cannot overwrite newer independent changes. Directory-tree deletions must check the reviewed descendant baseline and preserve unexpected new files.

## 6. Agent isolation and execution

### 6.1 Threat model

Treat agent commands, repository instructions, tool arguments, package scripts and agent messages as untrusted. Protect ungranted host data, other Envs, credentials, the broker, and source integrity. Isolation cannot prevent an authorized model provider from seeing content deliberately supplied to the agent, nor can it retract bytes already read or sent.

A CLI's own approvals or filesystem rules are useful additional controls, but are not the sole boundary. Prompts such as “use MCP only” do not prevent direct filesystem access.

### 6.2 Runtime profiles

| Profile | Intended guarantee | Draft status |
| --- | --- | --- |
| Isolated runtime | Per-Env process/filesystem/network restrictions; only private scratch and approved tools exposed | Required for calling an Env isolated |
| Restricted host process | Host-native CLI under an OS-enforced sandbox with equivalent tested restrictions | Platform feasibility work required |
| Host process / integration-only | MCP scoping, but CLI may retain host access | Explicit opt-in development mode; never advertised as isolated |

A private Linux container/VM runtime is the initial candidate for reliable tool compatibility and isolation. It may require a separately installed runtime and consume additional memory; it is not a promise of dependency-free native macOS execution. A container with host home, runtime socket, host PID namespace, privileged mode, or broad host mounts does not meet the isolation contract.

Native macOS, Linux, and Windows runtime adapters must each prove equivalent restrictions before being offered as isolated. Select actual sandbox APIs in an implementation ADR; do not depend on an undocumented mechanism or assume one platform's guarantees transfer to another. Fail closed when the selected isolation profile cannot start; never silently fall back to host execution.

### 6.3 Launch contract

A launch specification includes immutable Env identity, agent adapter/version, role instructions, runtime profile, resource quotas, scratch handle, narrowly scoped MCP authority, allowed egress and explicitly approved secrets.

Before each agent launch, its adapter supplies the parameters/configuration required
to connect to **this Env's MCP server**. This is automatic: the user does not need
to copy an endpoint or configure MCP manually. Resolve the virtual executable to
its stable Space, Env and AgentEntry IDs, establish a fresh session binding, start
or reuse the trusted Env MCP service, and generate the adapter-specific launch
configuration before starting the CLI. Pass the stdio gateway command/arguments
or supported endpoint configuration through the CLI's documented configuration
mechanism; exact flags are adapter/version-specific and must be tested rather
than assumed identical for Codex and Claude.

The binding is scoped to the Env but attributed to this particular agent session.
Use a protected inherited channel or private per-session configuration for secrets;
never place bearer credentials in visible command-line arguments, panel entries,
terminal output or shared project files. Nonsecret connection parameters can be
passed as ordinary arguments where the adapter supports them. The broker validates
the launch-bound authority, not a caller-supplied Env ID. Reopening a running console
reuses its existing MCP connection; restarting after exit creates a fresh binding.
Validate gateway readiness and configuration, and report failed MCP initialization
in the agent console with retry/reconfiguration. Do not fall back to unscoped host
filesystem access if MCP setup fails. Runtime policy still enforces isolation even
when an agent client fails to initialize its tools.

Start with a clean environment and private HOME/config/cache directories. These are per AgentEntry, persist across FarshMac restarts and are separate from disposable scratch, because the CLI keeps its own session store there (§6.4). Keep the agent's working-directory path stable across launches; a CLI may key stored sessions by it. Do not forward the user's SSH agent, kubeconfig, Docker socket, Keychain access, cloud credentials or general proxy settings. Agent authentication must use a reviewed adapter-specific mechanism with minimal exposed credentials. Shared provider login and token renewal remain an implementation decision, not a reason to mount the user's home.

Permit only required model-provider endpoints by policy, with optional user-approved package/repository access. Account for DNS, redirects, local/LAN addresses and proxy bypass; an allowlist is not proof against exfiltration to an allowed destination. Third-party MCP servers get separate grants and runtime policies; avoid inheriting the trusted broker's host authority.

Track the entire process tree. Stop interrupts the agent, then terminates remaining descendants after a timeout, cancels MCP operations and revokes the session. Suspending a console is distinct from stopping its processes. Background CPU, memory, disk, process count and output quotas remain enforced.

### 6.4 Session continuity across restarts

In the initial implementation agents run under the FarshMac process; an out-of-process supervisor is deferred (§12.1). Quitting, crashing or updating FarshMac therefore ends agent processes. Continuity comes from each CLI's **own session management**, not from keeping the process alive or reconstructing terminal state.

- The adapter records the CLI's native session reference in AgentSession as soon as it is reliably known: an ID the CLI reports through a documented mechanism, or one assigned at launch where the CLI supports that. Do not scrape it from terminal text when a documented mechanism exists.
- After a restart the agent entry shows a *resumable* state. Enter starts the CLI through the adapter's documented resume mechanism for that reference. The CLI restores and redraws its own conversation; FarshMac does not replay or snapshot the previous screen.
- Agents are never resumed automatically at application launch. Resuming is a user action, like starting.
- A resumed launch is a new process with a fresh MCP session binding and authority generation (§6.3). The previous binding is revoked at shutdown, or during reconciliation after a crash. Grants are evaluated as they are now: a source detached in the meantime stays inaccessible.
- Not restored: running subprocesses, in-flight tool calls, processes in a materialized workspace, and MCP responses the agent never received. The operation journal and idempotency keys (§7) let a resumed agent find out whether a mutation was committed; nothing is replayed automatically.
- Console output from the previous process is kept as a read-only transcript under the retention policy, separate from the resumed console.
- If resume fails (session store missing, CLI version changed, reference rejected), report it in the console and offer a new session. Do not silently start a fresh conversation in place of the old one.
- **Stop agent** also leaves a resumable session; restarting offers resume or a new session. Removing the agent (F8) disposes of its session store under the retention policy.

Resume support, session-reference discovery and session-store layout differ by adapter and version. Verify them per adapter; an adapter without a reliable resume mechanism offers only a new session and says so.

## 7. MCP surface and authority

Expose one logical MCP server per Env, backed by the common trusted service. A lightweight stdio adapter is the initial candidate: the CLI launches it inside its runtime; it forwards requests over an authenticated, private channel to the host broker. It has no direct source access. A runtime bridge must carry only Env-scoped RPC, not a general host filesystem/socket proxy.

The implementation must choose a supported MCP revision and test negotiation with each CLI adapter. Transport/authentication details follow that revision. Do not treat an MCP transport session identifier as an access credential. HTTP, if added, requires the applicable authorization flow and endpoint validation; loopback binding alone is not authorization.

The broker derives Space/Env/session IDs from launch-bound authority, validates it on every call, and checks the current grant generation. Removing a source or stopping a session invalidates future reads, open streams and pending mutations. Inaccessible entries disappear from listings and search. Existing materializations or already-read data cannot be retracted; the UI must distinguish that limitation and offer stop/cleanup.

Proposed names below are application tools, not additions to the MCP standard:

| Tool family | Example operations | Controls |
| --- | --- | --- |
| `vfs_*` | stat, list, read, search, write, mkdir, remove, rename, diff | Scoped virtual paths; pagination; byte limits; expected revisions |
| `artifact_*` | create from revision, inspect, accept into overlay | Immutable manifests; per-recipient grants; no implicit source publication |
| `workspace_*` | materialize, import selected changes, cleanup | Scratch quota; pinned baseline; explicit capability |
| `space_*` | list permitted peers, send/read/ack messages, inspect/update assigned tasks | Authenticated sender; same-Space policy; revision checks |
| `operation_*` | inspect progress/result, cancel | Owner checks; idempotency keys; bounded retention |

Resources may expose role instructions, source descriptions and permitted artifact manifests. Never expose a host file URI or absolute source path merely to help the model navigate. MCP roots/tool descriptions can aid clients but do not enforce access.

Use structured errors (`not_found`, `permission_denied`, `conflict`, `quota_exceeded`, `source_unavailable`, `cancelled`) without leaking protected host paths. Long jobs return operation IDs, progress and resumable cursors; cancellation maps to the shared operation context. A timeout does not authorize blindly repeating a committed mutation. Every mutating request has an idempotency key scoped to the authenticated Env.

Audit metadata records actor, virtual target, operation ID, decision and revision. Contents and credentials are not logged by default. Cap message, request, search and response sizes independently of CLI behavior.

## 8. Messages, tasks, and handoffs

Messages are durable, ordered within a recipient mailbox by broker-assigned sequence, and delivered at least once. Clients deduplicate by message ID and acknowledge explicitly. Sending uses idempotency keys; restarting an agent must not create duplicate messages or tasks. Mailbox polling is the baseline; optional notifications are hints, not the only delivery mechanism.

The sender identity is assigned by the broker and includes AgentEntry and session IDs, not only the Env. An agent can message permitted agents or Env inboxes within its Space, not impersonate a user, another agent or another role. Recipient-agent delivery and Env-wide delivery are explicit choices; an Env inbox is not silently consumed by whichever agent happens to be foreground. Cross-Space messaging is disabled initially. A user can inspect queues, pause delivery, revoke sharing and stop agents.

Message contents are data, not privileged instructions. Delivery does not automatically run shell commands, start a dormant agent, accept a patch, or expand permissions. UI and prompts retain sender/provenance. Automatic wake-up and task chaining require a separate explicit policy with concurrency, budget and loop limits.

An artifact reference carries no authority by itself. Recipients require an explicit grant to immutable content from a specific Env revision. Sending a message cannot silently grant access to the producer's whole live filesystem. Grants cannot exceed the producer's share permission. Revocation blocks future access but cannot erase copies already consumed.

Tasks have proposed states `draft → ready → running → blocked → submitted → accepted/rejected`. Only permitted actors can assign, transition or accept tasks. Record acceptance evidence separately from claims in agent text. A test pass does not automatically approve publication to host sources.

Example handoff:

1. PM publishes requirements and acceptance criteria as an artifact.
2. Architect receives that artifact and approved source references; submits a design and task graph.
3. Developers receive specific tasks and independently produce changesets in separate Envs.
4. User/integration policy assembles a candidate revision with conflict resolution.
5. Acceptance Tester receives the candidate and criteria, materializes a test workspace, and submits results tied to that exact revision.
6. User reviews and publishes the accepted changeset to original sources.

## 9. Panel and console behavior

```text
AI Spaces/
  Example Project/
    Product Manager/
    Architect/
    Developer A/
    Acceptance Tester/
```

The primary entry point is **Alt+F1 / Alt+F2 → AI Spaces**. Selecting it opens
an ordinary virtual panel listing all AI Spaces as folders: Alt+F1 targets the
left panel, Alt+F2 the right. This is panel navigation, not a separate chooser,
dashboard or modal list of projects. The Left/Right menus expose the same action.
The destination panel becomes visible and returns to its last ordinary file-list
mode if it previously showed Quick Look, metrics, or another special view.

Enter on a Space folder opens its AI Envs as folders. Enter on an Env opens its
effective filesystem root. `..` returns to the enclosing catalog level, restoring
the cursor to the folder just left. Standard panel selection, sorting and
navigation apply at each level; context actions depend on whether the current
object is a Space, an Env, or an entry in the Env filesystem.

The Space/Env catalog is a virtual backend. Store IDs rather than display names in bookmarks, recent connections and shell context keys. Renaming a Space or Env must not break them.

| Interaction | Behavior |
| --- | --- |
| Left/Right and Alt+F1/F2 → AI Spaces | Open the Space-folder catalog in the corresponding panel; recent Env locations use the existing recent-use ordering |
| F5 host → Env | Attach source reference; destination/collision dialog explains that no source data is copied |
| F5 Env → host | Export effective selected content with normal progress and collision handling |
| F5 Env → Env | Explicit artifact/reference-sharing choice; never grant the source's entire original root implicitly |
| F6 within Env | Virtual move/rename; never host mutation |
| F3/F4 | View/edit through `documentSource`; save to overlay with expected revision |
| F7 at the AI Spaces catalog root | Create an AI Space and focus its folder |
| F7 inside a Space | Discover available agents, select one or more, create an AI Env containing their executable entries, and focus its folder |
| Shift+F6 on a Space or Env | Rename the selected catalog object; preserve its stable ID, grants, bookmarks and session identity |
| F8 on a Space or Env | Delete the selected Env, or the Space and its Envs, without modifying original source files or directories |
| F8 on an agent executable | Stop and remove only that agent entry and session; retain the Env, shared overlay and other agents |
| F7/F8 on ordinary entries inside an Env filesystem | Create an overlay directory / hide or remove a virtual entry |
| Alt+F7, tree, Quick Look | Traverse only the effective Env view and respect cancellation/revocation |
| F11 / Alt+Enter | Add agents on an Env or from its filesystem context; start/stop agent, messages/tasks, changes/diff, materialize, revert, detach, export/publish where authorized |
| Enter on an agent executable | Start that agent if needed, or reveal its existing virtual console; focus input for conversation |
| Cmd+O | Toggle between panels and the associated retained agent console; hiding the console never stops the agent |
| Cmd+= | Open the same Env and virtual location in the other panel, without creating a new writer agent |

### Catalog creation, renaming, and deletion

Creation uses **F7** at the corresponding catalog level: the AI Spaces root creates
a Space; a Space folder creates an Env through the agent-selection flow below.
Ask for a name, validate collisions, and focus the new folder. Inside an Env filesystem, F7 retains its ordinary virtual
directory-creation meaning. **Shift+F6** renames Spaces and Envs without recreating
them or restarting their agents.

#### Agent discovery during Env creation

After **F7** inside a Space, FarshMac discovers supported installed console agents
and presents their names as checkboxes. The user names the Env and selects **one
or more** agents, for example `codex` and `claude`. Do not force a single choice.
Create the Env folder and all selected AgentEntries in one catalog transaction;
cancelling the dialog creates nothing. Merely creating an Env does not start any
agent or initiate provider login.

```text
AI Spaces/
  Example Project/
    Development/
      codex       [virtual executable]
      claude      [virtual executable]
```

Discovery belongs to agent adapters: inspect the resolved login PATH and known
installation locations, deduplicate aliases/symlinks to the same installation,
and perform only bounded, cancellable identification/version probes for recognized
candidates. Do not execute project-local scripts or automatically trust executables
found in a source folder. Show installation/version details on demand. Installed,
authenticated and runnable in the selected isolation runtime are separate states;
do not describe a detected host binary as runtime-compatible without validation.

The initial adapters recognize Codex and Claude; further supported agents appear
through the same registry. If discovery fails or finds no supported agents, show
a concise explanation with Retry and a manual executable-location option. Manual
selection still requires a supported adapter and executable validation; it does
not authorize arbitrary host execution. Never install software or change isolation
mode implicitly. An ambiguous installation choice is resolved explicitly, with
the choice stored in the AgentEntry. Agent authentication can be completed when
its executable is first opened.

Each chosen agent gets a distinct virtual executable named after its command,
without a required `.agent` suffix. Reserve these typed entries in the Env root:
source attachment or ordinary overlay writes with a colliding name must use the
collision dialog and cannot replace the launcher. Renaming the Env retains all
agent identities and running sessions. Missing or changed installations are
reported on launch with a reconfiguration action, not silently substituted.

Within one Env, `codex` and `claude` can both run. Opening either entry attaches
only its own session. **Cmd+O** restores the last console used in that Env; opening
a different executable changes that association. Runtime/session keys include
Space ID, Env ID and AgentEntry ID, so multiple agents never share terminal input,
output, credentials or lifecycle accidentally. Shared Env data remains explicitly
shared; private scratch is partitioned per agent. Stop agent affects only the
chosen agent; deleting the Env stops all of them.

#### Adding and removing agents in an existing Env

The Env context menu (**F11 / Alt+Enter**) includes **Add agents…**, both on an Env
folder in the Space catalog and while browsing that Env. It reuses discovery and
multi-select checkboxes from creation. Already configured agents are identified
and cannot be accidentally added twice; supporting multiple instances of the same
adapter is a separate future decision. Confirming creates only the newly selected
virtual executable entries, without replacing the Env, modifying its source grants
or overlay, restarting existing agents, or automatically launching new ones.
Cancelling leaves the Env unchanged.

**F8 on a virtual agent executable** is an explicit agent-removal operation, not an
overlay whiteout and not deletion of the containing Env. A concise confirmation
names the agent(s) and states that running sessions will be stopped. Revoke only
the selected agents' MCP session authority, cancel their in-flight operations,
stop their process trees, and detach their consoles in every window. Remove their
AgentEntries only after shutdown is confirmed; report failures with the standard
Retry / Skip / Skip all / Cancel behavior, retaining failed entries for recovery.
Stopping a shared runtime must never be used to remove one agent if it would stop
other agents in that Env.

Keep the Env folder, source references, shared overlay changes (including changes
previously made by the removed agent), other agent entries, and their processes and
consoles intact. Never delete original host files. Private scratch/transcripts follow
the agent retention policy; they are not aliases for shared Env storage. Retained
artifacts/messages preserve producer provenance even after the AgentEntry is gone.
Invalidate the last-console association if it pointed to a removed agent; Cmd+O
shows a no-active-console state until another executable is opened. Removing the
last agent leaves a valid empty-agent Env to which agents can later be added.

Multi-selection removes only the selected agent entries. Successful removals clear
their marks; skipped or failed entries remain selected. Removing an agent is distinct
from **Stop agent**, which retains its executable for a later restart, and from
closing its console, which leaves the agent running.

**F8** deletes Spaces or Envs using a concise confirmation identifying the selected
objects and the scope of removal. Deleting a Space includes its Envs. SourceBindings
are detached, not followed for recursive deletion: original files and directories
must remain untouched, including sources shared with other Spaces or Envs. Pending
overlay edits are not automatically applied to originals during deletion.

Before completing deletion, stop affected agent process trees, revoke their MCP
authority, cancel pending jobs and close their console attachments across windows.
Delete only application-owned catalog state, overlays and scratch storage according
to the retention policy. Never recursively delete a registered source path or follow
scratch symlinks into host sources. Immutable artifacts retained by another explicit
grant remain pinned until their own lifecycle permits collection. A failed stop or
cleanup must be reported and remain recoverable; it must not orphan a running agent
with usable access. Multi-selection follows the shared progress, retry/skip/cancel
and successful-selection clearing rules.

Acceptance tests must cover F7 at both catalog levels, identity-preserving rename,
Env deletion, recursive Space deletion, active-agent cleanup, and preservation of
original sources and data belonging to other Envs.

Show source-backed, added, modified, hidden/conflicted state using theme-compatible markers and a compact status column. An optional Changes view includes whiteouts. Quick Look shows provenance, virtual size/summary and pending changes; do not duplicate full source paths across every row.

Console status identifies Env, agent/runtime profile and actual scratch cwd. Virtual cwd and scratch cwd are distinct. Moving a panel in virtual data does not send `cd` to a host source directory. The UI can offer materialization or a scratch console; it must not pretend that an MCP-only VFS is a shell mount. Panel command entry routes to the selected supported console and explains unavailable execution contexts.

### Agent executable and retained virtual console

Each selected agent is represented by a virtual executable file inside its Env, for example
`codex` or `claude`. It is
visually identifiable as executable and has a stable agent ID independent of its
display name. Its panel metadata can show stopped, starting, running, waiting for
input, or exited state when that state is reliably available from the adapter.
Do not infer precise task completion solely from terminal text.

Pressing **Enter** on this entry means **open this agent's console**:

- If the agent has never started, start it under the Env's runtime/MCP policy,
  create its retained virtual console, and attach the current window.
- If it is starting or running, attach to the existing session and reveal its
  output, scrollback and input. Never launch a duplicate process because the entry
  was opened again or opened from another window.
- If it exited or failed, or was stopped by an application restart, reveal its
  retained output and exit status and offer an explicit restart: resume the
  agent's own session (§6.4) or start a new one. Preserve the prior transcript
  separately when restarting.

The console is interactive: the user sees the actual CLI and can communicate with
it using normal terminal input, selection and copy/paste. Closing or hiding the
console view detaches presentation only; **it does not terminate the agent**.
**Cmd+O** returns to the panels while the agent continues running. The user can
navigate into another Env and press Enter on its executable to reveal that Env's
console, starting its agent only if needed. Returning to the first Env and opening
its executable restores the same running session and output. A hidden agent that
needs input remains alive and is indicated in the panel; it does not steal focus.

Panel location changes select the appropriate retained console context without
stopping the previously visible agent. Cmd+O must not expose a different Env's
console or start a local shell as a fallback. If an Env has no console yet, present
an empty console state explaining that one of its agent executables must be opened first;
merely visiting an Env does not start an agent.

Explicit **Stop agent**, F8 deletion of the agent executable, Env/Space deletion, application shutdown policy, runtime
failure, or the CLI exiting can end the process. Closing a console view, changing
panels, or closing an attached window cannot. In-process retention lasts while
FarshMac is running. Application quit or update stops agent processes; their
conversations are resumed on demand through each CLI's own session management
(§6.4). Application quit must distinguish itself from closing a console and
explain that active agents will stop and can be resumed at the next launch.

The executable entry is not ordinary overlay content. F3 may show agent role,
adapter and runtime configuration; F4 may open a validated settings editor rather
than arbitrary executable bytes. Copy/export/materialize must never turn the entry
into a runnable host command or copy session credentials. Ordinary source files
cannot forge this entry type, and virtual file permissions alone cannot authorize
agent execution. F8 dispatches to the agent-removal lifecycle above rather than ordinary file
deletion. Entry renaming is an agent-configuration operation that preserves its
stable identity and must not silently kill a running process.

Acceptance tests must exercise starting once, opening the same entry repeatedly,
Cmd+O while output continues, switching between running agents in two Envs,
closing/reopening console views and attached windows, bounded background output,
multiple-window input ownership, explicit stop, and reopening an exited agent. Also
verify discovery, multi-agent creation and cancellation, launcher-name collisions,
two concurrent agents in one Env, per-agent scratch separation, last-console
restoration, and revision conflicts between agent/editor writes. Verify Add agents
on an existing Env, duplicate prevention, removal of one running agent while another
continues, removal of the last agent, shutdown failure recovery, and retention of
shared overlay edits. Launch tests must verify automatic MCP configuration for each
adapter, correct Env/session binding, isolation between agents' credentials, failed
initialization without host-access fallback, and revocation after removal.

Existing `shellsession` owns presentation/transport state per window today. AI sessions need service-level ownership plus window attachment handles so closing any attached view or window does not kill its agent, even when no views remain. Allow one input owner at a time with explicit takeover. Reuse bounded terminal history and background draining. Application quit prompts for active agents; initial implementation stops them and relies on native session resume (§6.4) rather than promising an unmanaged background daemon.

Batch mutations use determinate/streaming progress and Retry / Skip / Skip all / Cancel. Successful items are unmarked; skipped and unprocessed items remain selected. Source attachment, publication and revocation have distinct verbs so ordinary Delete cannot accidentally affect original data.

## 10. Recovery, limits, and observability

Persist catalog mutations and message enqueue operations transactionally. Overlay content is staged, flushed and atomically referenced only when complete. On restart, mark lost agent sessions interrupted (resumable where a native session reference exists, §6.4), revoke their old MCP bindings, terminate agent processes orphaned by a crash, reconcile runtime processes, recover committed operations, and discard incomplete staging files safely. Do not replay non-idempotent tool calls automatically.

Define per-Env limits for scratch/overlay bytes, file count, active jobs, messages, retained output, read bandwidth and index size. Default values are an implementation decision that must be measurable and user-visible. A full disk produces a resumable error; it never silently falls back to modifying sources.

Expose agent/session state, active tools, pending decisions, storage usage, mailbox depth and last errors in Info/Quick Look. Audit events use bounded retention and redacted fields. Metrics and logs must not become an alternate path for reading another Env's content.

## 11. Delivery plan and acceptance gates

### Phase A — Domain and overlay, no agents

Implement catalog, local source registration, lazy traversal, overlays/whiteouts, revisioned editor saves, search, diff, export and basic panel integration. Test millions of entries without eager scans, large files with bounded memory, and cancellation on navigation. Confirm delete/rename never touches a host source.

### Phase B — Env-scoped MCP and scratch

Implement scoped read/write/search, operation cancellation, materialize/import and immutable artifacts. Test path escapes, symlink swaps, replaced roots, revoked grants, cross-Env IDs, pagination invalidation, quotas, retries after a lost response, and crash recovery. Keep a malicious test client, not just cooperative agent demos.

### Phase C — Isolated console agents

Choose one validated runtime and ship Claude/Codex adapters only after compatibility tests. Verify attempted reads of host home, SSH keys, other scratch roots, runtime sockets and unapproved network destinations fail. Test subprocess termination, application restart, multiple attached windows and unavailable runtime handling. For each adapter, test quit, crash and update followed by native resume; resume failure handling; absence of orphaned agent processes after a crash; and that a resumed session gets a fresh MCP binding with current grants. Document external runtime/CLI requirements honestly.

### Phase D — Collaboration and publication

Implement durable mailboxes, task views, recipient grants, artifact handoffs and conflict-safe application to originals. Test duplicate delivery, revoked artifact access, offline recipients, concurrent edits, deletion conflicts, partial publication and cancellation. Demonstrate PM → Architect → Developer → Tester using distinct grants and overlays.

### Phase E — Optional automation and platform expansion

Add approved wake-up/task policies and additional runtime/source adapters. Each platform requires its own isolation and recovery evidence. Native Classic/Modern presentation work may consume the same semantic model; it is not a prerequisite for the initial feature.

A feature is not accepted merely because a CLI can call MCP. The acceptance suite must demonstrate that commands cannot bypass the granted view in the selected isolation profile.

## 12. Open decisions

- First isolated runtime on macOS: external container runtime versus a managed VM, installation and lifecycle costs.
- Exact CLI versions, MCP revisions, login flow and credential sharing supported by each agent adapter.
- Live-reference default versus optional per-task Git snapshots; non-Git snapshot/storage tradeoffs.
- SQLite driver, encryption at rest, content retention and export/import format.
- Default exclusions such as `.git`, `.env`, credential files and nested mounts; visibility alone must not imply permission.
- Whether any agents may publish to originals under a future delegated policy; initial answer is no.
- Source grant renewal when an original folder moves, disappears or changes identity.
- Per-Env runtime sharing versus separate sandboxes; shared runtimes must not weaken filesystem or process isolation.
- How much agent-specific status can be structured reliably rather than inferred from terminal text.

- Native resume mechanism, session-reference discovery and session-store location for each agent adapter/version, including whether stored sessions are keyed by working directory.

### 12.1 Deferred: out-of-process supervisor

Hosting MCP, consoles and agent process trees in a separate long-lived process, managed exclusively from the FarshMac UI, would let agents keep running across a UI restart or update. This is deferred, not rejected; §6.4 covers continuity until then. Notes for when it is revisited:

- MCP must work without the UI, so the supervisor would also own the catalog, overlay store, source adapters and brokers as their single writer; the UI becomes a client.
- It stays headless: no window, settings or user decisions of its own. Operations needing a decision park until a UI answers.
- The control channel (UI ↔ supervisor) is separate from the MCP channel and must authenticate the UI in a way that excludes same-user agent processes.
- The supervisor owns PTYs and drains output without a UI attached. Correct redraw of full-screen CLIs after reattach is unresolved.
- A new UI must negotiate with an older running supervisor; schema migrations run only in the supervisor.
- The launch mechanism (detached re-exec or launchd agent) decides which macOS privacy grants the supervisor holds and agents inherit.
- Agents outliving the UI need visibility at quit and next launch, plus idle and budget limits.

To keep that extraction cheap, the initial implementation follows three rules: AI Spaces packages do not import `internal/ui` or `internal/native`; `internal/ui` reaches them through one service interface whose commands and events are serializable; and session ownership sits above individual windows (§9).

## 13. References and status discipline

Current integration references:

- [Shell session management](SHELL_SESSIONS.md)
- [Search strategies](SEARCH_STRATEGIES.md)
- [Context actions and multiple selection](CONTEXT_ACTIONS.md)
- [File operations](FILE_OPERATIONS.md)
- [Native UI and JavaScript plugin architecture draft](NATIVE_UI_PLUGIN_ARCHITECTURE_DRAFT.md)
- `internal/ui/panel_backend.go`, `internal/ui/document_source.go`, `internal/shellsession`, `internal/fsops`

Protocol references for implementation review:

- [MCP transport specification, versioned reference](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2025-11-25/basic/transports.mdx)
- [MCP authorization specification, versioned reference](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2025-11-25/basic/authorization.mdx)

MCP defines transport and authorization mechanisms; the overlay, grants, artifact rules and runtime isolation in this draft are FarshMac design proposals. Those linked versions are reference material, not a claim about the newest revision or a selected compatibility baseline. Confirm and pin the implementation's revision against the supported agents before coding the adapter.

Keep this document separate from shipped-feature documentation. Update decisions and tests as implementation proceeds; do not describe the proposed security profile as verified before its acceptance gates pass.
