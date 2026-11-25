# AI Spaces: efficient execution and review

Status: implementation notes for the local AI Spaces build, November 2026.
This follows the test3–test8 experiments. Test6 completed the same QueuePad task with 50.3% less combined uncached input than frozen test5. See [the test6 review](AI_SPACES_TEST6_REVIEW.md) for coverage, measurement boundaries and follow-up fixes that were not part of that measurement. [Test7](AI_SPACES_TEST7_REVIEW.md) exercised those fixes and a real application correction: evidence delivery and focused rechecks worked, while total uncached input grew 23.6% over test6. These are observational runs with different correction work, not a causal efficiency estimate.

[Test8](AI_SPACES_TEST8_REVIEW.md) exercised horizontal browser keys and batched
workflow status without API errors. Its first candidate passed all ten browser
cases; combined uncached input fell 19.4% versus test7. It needed no application
correction, so the difference is not an isolated measurement of the API changes.

[Test9](AI_SPACES_TEST9_REVIEW.md) repeated the four-role setup on the current
build. [Test10](AI_SPACES_TEST10_REVIEW.md) used Product + Coder with independent
Product-authored browser acceptance. Test10 found and corrected a real keyboard
bug, used 12.9% less combined uncached input than test9, and took essentially the
same time. Output increased 2.3%; prompts, model mix and correction work differ,
so these single observations do not isolate the effect of role count.

## Coordination

When creating an Env with F7, choose an installed agent and a role from the
dropdown lists. **No role** creates an Env without role instructions.
**New role…** asks for a name and opens the multiline prompt editor. Save the
prompt with F2 and return with Esc to select the new role in the Env form.
Cancelling an unsaved draft creates no template and preserves the Env form.
Custom roles are reusable and can also be managed under
**Options > AI Spaces settings > Role templates**. Creating an Env copies the
selected prompt into its `AGENTS.md` or `CLAUDE.md`; later template edits do not
change existing Envs.

The default role templates use one review loop:

1. Product defines observable acceptance criteria and delegates to Architect.
2. Architect checks required runtime capabilities before implementation, assigns
   Coder, then gives a runnable immutable candidate to Tester.
3. Coder implements and supplies developer checks. Tester authors independent
   cases. Architect owns defect consolidation, correction and affected rechecks.
4. Product receives one consolidated disposition, evidence and unresolved gaps.
   Scope changes and material blockers can still require an earlier decision.

Each role owns one concise living document, updated at handoff/completion.
Agents reply to the original assignment, end their turn after sending, and never
poll for peer replies. Configured complexity profiles remain user-controlled;
`workflow_status` distinguishes requested settings from observed model/effort
and token counters. Read it at handoff rather than on a timer. It exposes no peer
prompts or source files and makes no dollar-cost estimate.

Untouched built-in templates from the previous version are upgraded by exact
content fingerprint. Edited templates and existing Env instruction snapshots
are preserved. Existing sessions retain their earlier conversation context.
Model/effort changes use explicit CLI commands and wait for acknowledgement:
Codex `/model MODEL --reasoning=LEVEL` combines both fields; an effort-only
change omits the model. Claude uses `/model MODEL` and `/effort LEVEL` without
slider navigation. Personal CLI settings are protected from writes. Current
Claude Code rejects ordinary `/effort` levels when it cannot save a user
default; FarshMac then resumes the same native conversation with `--effort`.
Unsupported syntax also falls back to resume with launch arguments, keeping
the console, queue and output. New tasks explicitly request a fresh context.
Requested profiles remain distinct from model/effort observed in usage.
`/effort ultracode on` is Claude's separate workflow toggle, not an effort level;
workflow availability still depends on the agent's allowed tools.

## A small MCP surface

The host context menu offers **Go to real location** for a single visible file
or folder with backing storage. It opens the directory, or selects the file in
its real parent directory, in the same FarshMac panel. Virtual agent entries,
synthetic folders and multiple selections do not offer this action. Resolving
host paths is a UI-only capability and is not exposed through Env MCP.

Every Env advertises three stable tools, currently 1,149 bytes of schema JSON:

- `env_info()` returns bindings, contacts and operation names. Pass `names` to
  obtain only the needed operation schemas, up to eight at a time.
- `env_read({requests: [{name, arguments}, ...]})` performs up to twelve read-only
  operations with per-item errors. Responses have a 48 KiB text budget and a
  12 KiB item budget; callers receive an explicit truncation notice.
  `workflow_status` is supported in the same batch as file and artifact reads;
  it retains its Space scope, timeout and metadata-only response.
- `env_call({name, arguments})` dispatches one operation through its existing
  authority checks. Recursive dispatch and writes hidden in read batches fail.

Legacy operation names still work for already-running clients. Batched edits
remain available through `vfs_edit`. This reduces call overhead without changing
CLI approval policies, filesystem grants, peer contacts or directing rights.
Audit logs retain the actual dispatched operation and bounded arguments.

Recommended launches disable unrelated host apps/plugins and skills in Codex
through invocation-local configuration. Claude uses its Env MCP configuration,
recommended tools and no inherited settings sources. Built-in slash commands
remain available: `--disable-slash-commands` also disables `/effort` and `/clear`
in Claude Code 2.1.330 and cannot be used by the session controller.
Explicit CLI-default tooling remains an opt-in. Credentials and global CLI
configuration are not rewritten; mandatory CLI instructions still contribute
to context size. Additional manually configured Codex MCP servers are not
removed by these flags, except the host-only FarshMac operator, which remains
disabled inside an Env.

## Runtime preflight and browser checks

Architect calls `runtime_check` with `browser:true` once before a browser-dependent assignment if no shared result exists. Product and workers reuse the facts. Results are coalesced across Envs, cached for ten minutes and invalidated by process restart or Chrome replacement. `env_info.runtime` includes current cached facts; failures have a short retry TTL. Cancellation of a waiting caller does not block other callers.
It reports available tool versions and exercises navigation, JavaScript,
persistence after reload, upload, completed download and fresh-case storage.
Failure is reported as unavailable capability, never as a project test pass. Native select popup keyboard support is probed separately and reported explicitly; a DOM `select` action is not keyboard evidence. Unsupported popup probes must not be repeated by every role.

`browser_check` is a managed Chrome/Chromium service for **static source or build
folders**. It does not run an arbitrary backend or agent-supplied host command.
Builds and backend tests continue to use the isolated `workspace_run` process.
The installed native browser keeps its renderer sandbox; FarshMac never uses
`--no-sandbox`. There is no public arbitrary-JavaScript or CDP escape hatch.

The service stages only the caller's visible VFS files, or reads an explicitly
received immutable artifact. It uses a private browser profile, a loopback static
server authenticated with a per-run secret header and a fresh browser context/download
directory per case. Project staging and browser state stay outside agent scratch,
under FarshMac-owned state directories. Fetch interception
and CSP restrict page network access to that test origin. Frames, workers, objects
and popups are disabled. macOS Chrome initialization uses the real OS HOME, but
browsing state is kept in the disposable profile, not the personal profile.

Supported steps: `goto`, `click`, `fill`, `select`, `key`, `reload`, `text`, `value`,
`count`, `storage`, `upload`, `download`, `dialog`, `focus`. `text` uses full exact textContent by default; `match:"contains"` requests a substring. `focus` checks document.activeElement against the selector. Key events use focused/active page emulation and macOS key codes; all four arrow keys, `Shift+Tab`, Home and End are supported. Configure a dialog step as
`accept` or `dismiss` before the action that opens it. An expected dialog that
never opens fails the case. Reload waits for a new document; assertions retry
until their observable condition or deadline. Download checks wait for Chrome's
completion event and inspect only the current case's completed file.
`download` requires its trigger `selector` and performs the click itself. Set
`key:"Enter"` or `key:"Space"` on that step to test a keyboard-triggered download.
Do not trigger a separate download first. Missing selectors, storage keys,
invalid upload paths and invalid dialog decisions fail before browser startup.

Limits: two concurrent browsers, 20 cases, 80 expanded steps per case, 1,200 expanded steps per request, automatically split at case boundaries into batches of at most 240 steps. The whole request has a 120-second deadline; each step has five seconds. Specifications are limited to 1 MiB, resolved steps to 4 MiB, fixture files to 64 files / 1 MiB total, and inspected downloads to 1 MiB.
Cancellation/revocation stops the process group and removes temporary state.
Independent contexts do not prove repeated-download behavior within one long
session; use multiple download steps in one case if that is an acceptance criterion.

Prefer a reusable VFS JSON specification, written once to `/tests`, and call `browser_check({artifact: ID, spec: "/tests/browser.json"})`. The file contains `cases`, optional `page`, `fixtures`, `fixture_files` and `blocks`. For example:

```json
{
  "fixtures": {"board": {"version": 1, "tasks": []}},
  "blocks": {"seed": [{"action":"upload","selector":"#file","file":"board.json","value_fixture":"board"}]},
  "cases": [{"name":"imports empty board","steps":[
    {"use":"seed"},
    {"action":"text","selector":"#message","match":"contains","expected":"Imported"},
    {"action":"storage","key":"board","expected_fixture":"board"}
  ]}]
}
```

A fixture may be JSON or a string (including malformed JSON for a negative test). `fixture_files` maps names to VFS files, or files relative to the specification. Only the caller's VFS grants apply. References and blocks are fully resolved and validated before Chrome starts. Recursion, unknown fields, missing references and limit violations fail before execution. The resolved specification is retained with the run; the response includes the specification hash. No JavaScript generator or arbitrary evaluation is accepted.

For an affected-case recheck, use the same `spec` with `only:["exact case name"]`.
Names must exist and be unique; the result lists only executed cases. No copied
scenario or edited fixture is needed. Evidence stores resolved replayable input
separately from the original spec path/hash, so changing or deleting the spec
does not invalidate the recorded test inputs.

Small focused checks can still use inline cases (through `env_call` after schema discovery):

```json
{
  "name": "browser_check",
  "arguments": {
    "artifact": "0123456789abcdef",
    "page": "public/index.html",
    "cases": [{
      "name": "keyboard submission survives reload",
      "steps": [
        {"action":"fill", "selector":"#title", "value":"Example"},
        {"action":"key", "selector":"#title", "key":"Enter"},
        {"action":"text", "selector":".task-title", "expected":"Example"},
        {"action":"reload"},
        {"action":"text", "selector":".task-title", "expected":"Example"}
      ]
    }]
  }
}
```

## Versioned delivery and evidence

`workspace_run` records the actual exit status, duration, command, bounded output
tail and before/after content fingerprints **outside writable agent scratch**.
`browser_check` records the same metadata with structured case results. When called with an artifact ID it automatically attaches the run, including failures, to that same artifact. Reviewers return that ID; they do not republish the source to attach evidence. These are
server-issued run IDs; agents cannot submit an arbitrary pass status as evidence.

1. Coder runs checks on the final candidate and pushes it to its VFS folder.
2. `artifact_publish(path, name, runs)` captures an immutable version and binds
   run IDs only when both recorded fingerprints match that snapshot.
3. `agent_send`/`agent_reply` accepts `artifacts:[ID]`. Existing communication
   rights still apply; recipients receive access to those versions only.
4. `artifact_read` supplies a compact manifest, bounded file contents, or details
   of a specified run. The host operator also supports `artifact_read` for a
   version delivered to the user.
5. Tester may use `browser_check(artifact=ID)` directly, or `artifact_checkout(ID)`
   into a new private workspace followed by `workspace_run` on the returned path.
6. For isolated workspace checks, `artifact_verify(ID, runs)` appends your matching server-recorded evidence. A run owned by another Env must be attached by that Env. Browser evidence needs no extra verify call. Failed
   runs remain visibly failed; stale or input-mutating runs cannot attest a final
   candidate. A successful exit is execution evidence, not proof of sufficient
   coverage or Product acceptance.

Reading a run returns status, provenance and output without repeating the
potentially large resolved browser input. `command_bytes` and `output_bytes`
describe the durable full records; `output_truncated` explicitly marks a preview.
Use `part:"command"` or `part:"output"` with the same artifact/run IDs for detail.
These pages use **byte** offsets (default 1), at most 8,192 requested bytes,
and return `text` plus `next_offset` (0 means complete). File reads keep their
existing line offsets. This keeps review results visible within the `env_read`
item budget while preserving access to the exact command/scenario.

Original source bindings are never added to Product merely for acceptance.
Artifacts omit hidden overlay entries, symlinks, `.git`, `node_modules`, and
runtime caches/logs; lockfiles remain included. A snapshot contains at most 5,000
files, 128 MiB total and 16 MiB per file. The manifest lists up to 200 paths; a
reviewer can check out the version for larger-tree inspection. Publication is a
content snapshot, not an atomic transaction over an actively changing source tree.
Concurrent writers must finish before publishing and checking a candidate.
Snapshots/evidence are retained locally; automated artifact expiry/garbage
collection is not implemented yet.

## Validation

The regular Go suite covers compact dispatch, read-only/contact permissions,
partial batch results, artifact access and traversal, immutable source versions,
checkout confinement, stale run rejection, observed-versus-requested settings
and preservation of customized role templates. Race detection is supported.

Native integration checks are explicit and require installed Chrome/Chromium:

```sh
FARSHMAC_NATIVE_BROWSER_TEST=1 go test -race ./internal/aispaces \
  -run TestNativeBrowser -count=1 -v
```

They exercise independent repeated downloads, confirmation acceptance/cancellation,
blocked cross-origin requests, a deliberately failing assertion, cancellation and
binding browser evidence to its exact artifact. No model calls or test agents are
started by these tests. Run `./scripts/check.sh --race` for the full application
checks; native browser tests are skipped unless explicitly enabled.
