# AI Spaces: findings and approaches

**Status:** Record of the proof of concept; not shipped-feature documentation\
**Date:** 2026-11-21\
**Scope:** What was measured, what was tried, what was chosen and why, and what is still unverified, for agent cost, messaging, the operator server, directing and isolation\
**Companion to:** [AI Spaces Architecture](AI_SPACES_ARCHITECTURE_DRAFT.md). That draft states the intended design and lists what the PoC implements; this document keeps the reasoning and the numbers behind the PoC's choices.

Every number here comes from a small sample: six agent sessions in two Spaces, most of them recorded on early builds, and a handful of runs of a real CLI. Treat them as direction, not as benchmarks. Where something was reasoned but not measured, it says so.

## 1. Where an agent's cost goes

### 1.1 Method

Each CLI keeps a transcript with token counters per model step. `usage.go` reads them, and the counters are weighted by the price ratios of the model that produced them, with ordinary input as 1:

| Token kind | Weight |
| --- | --- |
| Output | ×5 |
| New context, 1-hour cache write | ×2 |
| New context, 5-minute cache write | ×1.25 |
| Context re-read from cache | ×0.05 (Opus 5.5, Sonnet 5.5), ×0.025 (Fable 5.1 and 5.2), ×0.1 otherwise |

The result is a cost in "token units". Two mistakes were made on the way and are worth remembering:

- Comparing raw token counts makes re-reading look like everything (about 48M cached tokens against 0.37M output tokens in the sample). Weighted, it is the largest single item but not the bulk.
- A first version used ×0.1 for cache reads on every model. With the right ratio the conclusions about short sessions reversed (§1.4).

Two counting errors were also fixed: transcript records that repeat an API request must not be counted twice, and "the same text read again" must compare whole reads, not repeated lines inside one file.

### 1.2 What the sessions showed

| Agent role | Model steps | Context, start → end | Cache read | Output |
| --- | --- | --- | --- | --- |
| Coder | 123 | 40K → 332K, two compactions | 24.4M | 186K |
| Architect | 91 | 41K → 251K, two compactions | 12.7M | 86K |
| Product | 64 | 41K → 127K | 4.9M | 41K |
| Architect (second Space) | 42 | 41K → 160K | 4.5M | 42K |
| Coder (second Space) | 19 | 41K → 100K | 1.3M | 12K |

Weighted cost by token kind: re-reading the context 39% (12 points of it are the CLI's own prompt and tool definitions), new context 30%, output 30%.

By what filled the context: file reads 25%, text the model typed into files 30%, messages 12%, console prose 10%, the CLI's own prompt and tools 12%.

Observations in the logs that led to changes:

- Reads were 779 KB of about 905 KB of all tool output. One architect read the same four source files three times while a coder worked on them. Text read again in the same context was 22–27% of reads in the older sessions and 0–1% in the newer ones, so this varies a lot with the task.
- One agent's first call returned a 502-line, 24 KB tree that stayed in its context for the whole session. One search in one document returned 16.8 KB because of long Markdown lines.
- An architect extracted document structure with a heading regular expression and then read by offset: it wanted an outline and had no tool for it.
- Agents typed 143 KB into replies and 60 KB into requests; 4–5 KB answers were normal. Typed text is paid as the sender's output and again as the recipient's input.
- An agent outside the Env asked for a file "verbatim" and the agent retyped 4.7 KB, because the operator server could not read the file itself.
- `vfs_write` into a missing folder failed; the agent created the folder and typed the same 5.5 KB again.
- Every session spent 3–4 steps on the CLI's tool search because the Env's tools were loaded lazily, and started at about 41K tokens before any work.
- Eight calls failed with "not attached to this Env": a colleague had quoted a reference the recipient did not have.
- After `agent_send`, 35–41% of two agents' console text repeated the message they had just sent.

### 1.3 Ideas considered

The ten ideas from the first review, with what became of each:

| # | Idea | Outcome |
| --- | --- | --- |
| 1 | A new session per task | Rejected as a default (§1.4). Kept as a deliberate act: a directing agent's `new_task` (§4) |
| 2 | Model and effort per role | Done: per-agent settings, then levels and explicit choices set by a directing agent (§4) |
| 3 | Do not resend unchanged files | Done: `[unchanged]` or a unified diff |
| 4 | Bound `vfs_list` and `vfs_search` output | Done: folded tree, grouped and cut search |
| 5 | An outline tool | Done: `vfs_outline`, `vfs_read` with `section`. Whether models reach for it is unverified |
| 6 | Long messages as files | Done differently: attachments the server fills in, not a size limit on messages |
| 7 | File access for the outside agent | Done: `message_read`, and real paths in what the operator is shown |
| 8 | Remove errors that make an agent retype | Done: `vfs_write` creates missing folders |
| 9 | Load the Env's tools up front; fewer built-in tools | Done; this was the largest measured gain (§1.6) |
| 10 | Offer to attach an unreachable reference | Partly: the sender is warned and the user is told which folder to attach. No one-key attach |

Left out: many single-tool steps of a coder (mostly `Bash`) are a matter of instructions, not of the API; and message exchange under the sandbox once looked 2–3 times slower than without it, which the logs could not explain and nobody measured again.

### 1.4 Theories that did not survive checking

- **Messages as tokens.** The proposal was to keep messages as durable objects and pass only an identifier through the model. Checked against the logs, this saves the recipient nothing: it reads the body anyway, and the extra call costs a full pass over its context. The saving is only on the sender's side, where a file would otherwise be retyped. So messages became durable, attachments are filled in by the server, and the body is still delivered inline.
- **Short sessions.** With cache reads at ×0.05, carrying a long context is cheaper than it looked; throwing it away means paying the new-context price again for whatever the next task needs. Not adopted as automatic behaviour.
- **A 5-minute cache.** Simulated on the recorded sessions, it would have raised input cost by 32%: agents wait for each other longer than five minutes.
- **Per-role tool sets for the Env's own tools.** Agents that share one tool set can reuse each other's prompt cache. Splitting the set by role gives that up, so the Env tool set stays the same for everyone, and only the CLI's built-in tools are narrowed.
- **SQLite for messages and objects.** Plain files (`messages.jsonl`, blobs by hash) were enough and add no dependency. The architecture draft still proposes a database for the real design.

### 1.5 What the tools do now

The rule behind all of it: every byte a tool returns stays in the context and is paid again on each later step, and every extra call costs a pass over the whole context. So answers are bounded, and one call should be enough.

- **Listing.** `vfs_list` returns an indented tree and folds folders to a count beyond 150 lines.
- **Outline.** `vfs_outline` returns headings or declarations with line numbers, for a file or a folder.
- **Reading.** `vfs_read` takes a line range or a `section`. A file the conversation already holds is answered with `[unchanged]`, or with a diff if it changed; `full=true` overrides. The server learns that a conversation was compacted or cleared from the CLI's hooks (§5) and then sends the text again.
- **Search.** `vfs_search` groups matches by file, cuts long lines around the match, and stops at 5 KB with per-file counts of the rest.
- **Writing.** `vfs_write` creates missing folders. `vfs_edit` applies a batch of replacements across files or nothing. `vfs_copy`, `workspace_pull` and `workspace_push` move bytes on the server so content is not generated twice.
- **Messages.** Kept per Space. `agent_send` and `agent_reply` take `attach`: a file, a line range, a section, or a thread to forward. Small attachments arrive inline; larger ones are listed and read in parts with `msg_read`. A message also lists the shared files its sender changed since its previous message. Agents are told not to repeat a sent message in their console.
- **Launch.** The Env's tools are loaded up front, and the CLI is offered only `Bash, Read, Edit, Write, AskUserQuestion, TodoWrite` of its own tools unless the agent's settings say otherwise.

Proposed and not built: a change feed with a cursor, base revisions on `vfs_edit` (which would also give conflict detection between writers), stored oversized results with a follow-up read, agent notes with a `resume` summary, `vfs_append` and patch input, and an explicit task state on a thread.

### 1.6 Measured with a real CLI

| Measurement | Before | After |
| --- | --- | --- |
| First context, headless | 26.3K tokens | 11.4K |
| First context, interactive | about 41K | 15–17K |
| One fixed task, weighted cost | 60.9K units, all built-in tools | 30.9K, narrow set |

The compaction hook was confirmed: after `/compact` the server sends a file's text again instead of `[unchanged]`. These runs cost about 0.15M units.

Not measured: a before/after comparison on the user's own scenarios. A baseline report of the earlier sessions exists; a resumed session mixes old and new steps in one transcript, so the comparison has to be cut by time.

## 2. Checking efficiency

Three levels run from cheap and always on to manual, plus probes that spend real tokens.

1. **Priced scenarios** (`TestEfficiencyScenarios`). Each scenario is taken from a real log and has the old way and the new way of doing the same thing. Both run against the real tools; a meter prices the bytes that would pass through the model (output ×5, new context ×2, re-reading ×0.05 per step over a 40K-token base, plus what the scenario leaves in the context for 20 more steps). A scenario fails when the new way exceeds its share of the old one or its absolute budget. Budgets go down when a tool improves.

   | Scenario | Old way | Now | Allowed share |
   | --- | --- | --- | --- |
   | Learn the structure of the documents | 157,420 | 3,670 | 10% |
   | Search documents with long lines | 51,258 | 6,065 | 30% |
   | Find one rule in the documents | 30,052 | 5,236 | 30% |
   | Hand over a long answer | 17,364 | 6,993 | 50% |
   | Review a colleague's change | 12,440 | 3,201 | 35% |
   | Write a file into a new folder | 32,529 | 15,096 | 50% |
   | Orient in a large project | 6,445 | 3,120 | 60% |

   These are a model of cost, not a measurement.

2. **Surface budget** (`TestToolSurfaceBudget`). What every agent pays on every step whether it uses it or not: Env tool definitions at most 11,000 bytes, Env server instructions 1,700, agent notes 2,300, operator tool definitions 4,200, operator instructions 1,100. A new parameter has to fit, which keeps descriptions short.

3. **Efficiency report** (F11 → **Efficiency report**). Read from the agents' own transcripts: steps, context growth, token counters, weighted cost, what filled the context by cost, and four signals: file text read again in the same context, console text right after sending a message, tool searches, tool errors.

   A fourth level sits outside the suite:

4. **Real CLI probes** (`FARSHMAC_AI_CLAUDE_PROBE=<built farshmac>`). `TestClaudeEfficiency` starts a real CLI exactly as FarshMac does on fixed tasks and reports steps, tokens, cost and the tools it chose; `TestClaudeReportsCompaction` checks the hook wiring; `TestClaudeIsDirectedInPlace` checks directing. They spend real tokens and are not part of the suite.

## 3. The operator server

**Need.** The user works with an agent outside any Env (Codex, Claude Code, Claude Desktop) and wants it to see Spaces, Envs and agents by name, look at what an agent is doing, learn when it finished, and write to it.

**Approach.** A second stdio server, `farshmac --ai-mcp-operator`, separate from the Env gateway.

- It speaks for the user, so Env communication rules do not bind it, and it may direct any agent.
- The gateway process answers the MCP protocol itself and forwards only tool calls. It finds the running application through `ai-spaces/operator.json` (socket and token, mode 0600, unreadable inside the agent sandbox), so the outside client keeps working across FarshMac restarts.
- Tools: `spaces_list`, `agent_status`, `agent_send`, `agent_wait`, `message_read`. It has no file tools; it is given real paths instead, because its client has its own file access.
- Agents answer with the ordinary `agent_reply`; replies wait until read.
- Addressing: `Env/agent`, an Env name when it has one enabled agent, or an agent name when it is unique in the Space.

**Found in use.** Claude Desktop needs `args` as an array in its configuration and must be restarted after FarshMac is rebuilt, or it keeps serving the old tool list. "Finished" was first guessed from a console that fell silent for three seconds; it now comes from the CLI's hooks (§5), and the silence rule remains only for CLIs without them. The transcript view is Claude-only.

## 4. Directing another agent

**Need.** An architect should set a coder's model and effort to match the task, clear its context before a new task, and choose how the coder works on it. The mechanism must hide which CLI the recipient runs, and must change the running agent rather than replace it.

### 4.1 Model

- **Permission.** Contacting and directing are separate. The user allows an Env to *direct* another in F11 → **Communication…**; this is one-way. Without it the parameters below are ignored, the message is delivered as an ordinary one, and the sender is told why.
- **Parameters of `agent_send`.** `complexity` (`simple`, `moderate`, `complex`), `model`, `effort`, `new_task`, `method` (`direct`, `workflow`, `research`).
- **Levels.** A level is a model and an effort per agent, set by the user in **Agent settings…**. Claude defaults: sonnet·low, opus·medium, opus·high. Codex defaults change effort only.
- **Explicit choices.** `model` and `effort` override the level and stay until the same sender names a level or another value. They are checked against what the recipient's CLI accepts: effort from the adapter's list, a model as a plain name. `agent_peers` lists, for each agent the caller directs, what may be named.
- **Timing.** The message waits in the console's queue until the recipient is between turns. A stopped agent is started with the settings.

The first version offered levels only, on the idea that an agent should not need to know another CLI's model names. In use, an architect reported that it "cannot set model and effort": the permission had not been given, and nothing in the tool said what was possible. Hence the explicit parameters, the list in `agent_peers`, and a plain statement there when the caller directs nobody.

### 4.2 Adapters

`control.go` holds one adapter per CLI: default levels, the command that clears a conversation, how to change model and effort in a running session, launch flags, accepted efforts, model hints, and methods.

**Claude Code, changed in place.** Verified against a real CLI:

- `/clear` starts a new conversation. The hook reports the new session ID, which becomes the one to resume.
- `/model <name>` is typed. When the conversation has history, the CLI asks to confirm the switch and Enter is sent.
- A typed `/effort <level>` fails when the settings file cannot be written. The slider is used instead: `/effort`, wait for its session-only hint, move to the left end, step right to the level, press the session-only key.
- Both commands would otherwise be saved as the user's defaults for every session. The sandbox denies agents writes to the user's shared settings, instructions, key bindings, commands, agents, skills, plugins, hooks, output styles and rules, and the change then holds for the session alone.
- Result of the probe: an agent on opus·medium received a simple new task and answered on sonnet·low in a cleared conversation (17K tokens), in the same process, with the user's settings file unchanged.

**A CLI with no in-place way** (Codex) is restarted behind the same console with flags. This is a fallback, written but not run against a real Codex. A first implementation restarted Claude too; that was rejected, because replacing the process is not what "change the model of a running agent" means.

**Methods.** `workflow` appends the `ultracode` keyword to the message; `research` starts it with `/deep-research`. Both need built-in tools an agent is not given by default (`Workflow`, `Agent`, and for research `WebSearch`, `WebFetch`) and are refused with the reason otherwise. A CLI's tool set cannot change in a running session, so enabling these means paying for their definitions on every step. Neither has been run against a real CLI; a workflow's approval question would wait for the user in a console that may have no window.

### 4.3 Addressing and ambiguity

- A message always goes to one agent, addressed `Env/agent`. An Env name alone works while the Env has one enabled agent; otherwise the sender gets the exact addresses and nothing is delivered.
- An agent's name is its CLI's name, so an Env holds at most one `claude` and one `codex`. Two coders on the same CLI are two Envs.
- The user can **disable** an agent (F11 on it). It is stopped, not listed, not addressable and not started by a message, which makes the Env's short address unambiguous again.
- A **contact** link is two-way: setting or clearing it on one Env does the same on the other. Links saved before this change stay one-way until saved again.
- Permissions belong to the Env and cover both of its agents; level, model, effort and conversation belong to each agent.

## 5. Knowing what an agent is doing

The CLI is launched with a settings file whose hooks run `farshmac --ai-mcp-event <event>`, which notifies the Env's server:

| Hook | Event | Used for |
| --- | --- | --- |
| `SessionStart` (compact, clear), `PostCompact` | `forget` | The conversation no longer holds what it read; on clear, adopt the new session ID |
| `UserPromptSubmit` | `busy` | A turn started |
| `Stop`, `StopFailure` | `idle` | Between turns: queued messages and settings may be applied |
| `Notification` | `notify` | A permission question (the agent waits for the user) or an idle prompt |

Rules that came out of review: an event from an earlier launch of the same agent is ignored; a session ID is adopted only on clear and only if it is a UUID; the hook command quotes its arguments. A turn interrupted with Esc may leave the agent looking busy until the next notification.

## 6. Isolation findings

- **Profile.** Agents run under `sandbox-exec`: write access to their scratch folder, device nodes, the per-user temp folder, the login keychain and the CLI's own configuration; no read access to the rest of the home folder. Project files reach an agent only through the Env's server. The launch fails closed if the sandbox is unavailable. `sandbox-exec` is deprecated by Apple; this is a PoC mechanism.
- **Keychain.** Without write access to the keychain folder the CLI could not save a refreshed login and reported that it could not save to the Keychain. Write access was added.
- **Attachments cross Env boundaries.** An attachment is a copy the sender pushes. It gives the recipient no access to the folder it came from. The alternatives were to allow attachments only from folders both sides have, or to ask the user each time.
- **Open, deferred by decision.** An agent can still write `~/.claude.json`, other files under `~/.claude` and `~/.codex` that the user's unsandboxed sessions trust. A deny list cannot close this; it needs a private configuration folder per agent and a separate way to give it the login. To be taken up only if it causes problems.
- **Not evaluated.** A container runtime for agents was reviewed and put aside for now.

## 7. Review findings worth keeping

Independent review of the console rework found defects of the same few kinds; the fixes are in place, and the kinds are worth checking in any later change:

- A queued message removed before it was typed, and so lost if delivery failed.
- Closing a console racing a restart behind it, and two places deciding the same restart.
- A worker that never ended after its console did.
- Reporting the end of a stream before its error.
- A window resize arriving while the process was being replaced.
- Events accepted from a stale launch, and a session ID taken from unvalidated input.

One testing lesson: a probe binary was once silently stale because the UI package did not compile and the errors were filtered out of the build output. Build the probe binary and check the build's exit status before trusting a probe.

## 8. Not verified

- `workflow` and `research` with a real CLI.
- The Codex adapter end to end, including its restart path and whether its hooks or equivalents exist.
- Whether models choose `vfs_outline` unprompted; in the probes the model preferred search.
- The gain on real multi-agent scenarios after these changes.
- The cause of slower message exchange under the sandbox.
- No budget or loop limit exists on agents starting or directing each other.


## Test4 follow-up implementation

The current follow-up is documented in [AI Spaces workflow](AI_SPACES_WORKFLOW.md):
compact operation discovery and batched reads, managed browser preflight/checks,
immutable candidate delivery with server-bound execution evidence, role-owned
correction loops, invocation-local context trimming, and observed model/effort/usage.
It supersedes the earlier suggestion to expand source shares for review: a
recipient can review or test a delivered artifact without attaching the source tree.
This is implementation and regression-test evidence; a new agent experiment is
required to quantify token savings and role compliance.
