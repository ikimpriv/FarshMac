# AI Spaces test7: correction loop and comparison with test6

Completed November 22, 2026, 12:14:21–12:29:13 America/New_York.

The four real FarshMac Codex agents completed a fresh QueuePad implementation
and Product accepted it after one application correction, without operator
intervention. Combined uncached input increased from **241,934 to 298,919
tokens (+23.6%)** versus frozen test6. Output increased 21.5%; elapsed time
increased from 12m 01s to 14m 52s.

The latest fixes improved evidence delivery and focused verification in actual
use. They did **not** produce a cheaper complete run in this observation. Test6
had no application correction; test7 exposed and repaired a real Clear filters
defect. Final functional coverage is comparable, while test7 supplies direct
evidence that the independent review and correction loop works.

## Controlled setup

- New Space `test7` (`303089cbb4717f0e`) and empty project
  `~/src/test7-queuepad`. No old application or tests were copied.
- The same initial QueuePad assignment as test6, changing only the experiment
  name: task CRUD and validation; status movement; intersecting search/status/
  priority filters; real reload persistence; complete export and atomic import;
  keyboard controls, labels and empty states; reproducible local HTML/CSS/JS
  with Node and a lockfile. The same exclusions and soft response target apply.
- The same role grants, contacts and directing rights. Product coordinates
  through Architect; Architect directs Coder and Tester. Each role owns its
  documentation. No extra agents or weakened approval/sandbox policies.
- Product, Coder and Tester used `gpt-6.2-sol`, medium. Architect used
  `gpt-6.2-astra`, medium. Native CLI records confirm these settings throughout.
  Both experiments used Codex CLI `0.171.0`; Node `22.15.0` and npm `10.9.2`.
  This is not an adaptive model/effort experiment.
- FarshMac was restarted into the exact signed build validated after test6.
  Executable/source hashes match `test6/evaluation/post-run-build.json`.
  New Envs received current default role instructions, including small
  `vfs_edit` repairs and affected-case rechecks.
- One initial message to Product. Afterwards the operator only observed logs
  and messages. No worker instructions, application edits, verification on the
  agents' behalf, prompt changes or infrastructure fixes during the run.

The comparison uses archived test6, not resumed live counters. Root setup and
audit usage are excluded from both measurements. No new FarshMac implementation
changes, build, commit, push or release are part of this repeat.

## Execution and quality of the result

Twelve messages completed the full cycle:

1. Product wrote the seven acceptance criteria and delegated to Architect.
2. Architect performed one shared runtime preflight, wrote the architecture
   contract and assigned Coder.
3. Coder delivered a runnable immutable candidate with eight passing Node
   tests. Architect reviewed it and commissioned independent browser tests.
4. Tester found an application defect: Clear filters reset the inputs but left
   the board empty. Nine of ten browser cases passed.
5. Architect sent one consolidated correction to Coder. Coder changed only
   `app.js` and `index.html`, reran all eight Node tests and published a new
   source version.
6. Tester reused the same JSON specification with `only` selecting the affected
   case. It passed. Architect reviewed the evidence and Product accepted the
   corrected version with the remaining verification limitation explicit.

The defect was in native form-reset ordering. Rendering from a microtask queued
by the `reset` event could read the old filter values before the native reset
default action. The corrected ordinary button calls `filters.reset()` and then
renders synchronously in its click handler; Enter activation uses the same
path. The failing assertion was retained rather than weakened.

| Evidence | Source artifact | Result |
| --- | --- | --- |
| Workspace `865b98d2f5a40a5e` | `bb1ddd8715d1f1b4` | Eight Node tests passed. |
| Chrome `716813d88fb5c07d` | `bb1ddd8715d1f1b4` | Nine of ten cases passed; Clear filters failed. |
| Workspace `48ba63591d9d8a59` | `5932763fe81aec26` | Eight Node tests passed after the correction. |
| Chrome `81ef468d2f8f00be` | `5932763fe81aec26` | The affected filter case passed. |

Both browser runs used spec SHA-256
`5c27bb552274f2a1003f4b896fd014f302996fcf097fc0507ec15b18f74bb47c`.
The baseline resolved to 460 steps across ten cases; the focused recheck used
49 steps in one case. Run fingerprints match their attached source artifacts
before and after verification. The two source versions differ only in the
clear button type and its handler.

**All ten cases were not rerun on the final source.** Nine unaffected baseline
passes carry forward based on the reviewed correction scope, plus one passing
recheck on the corrected version. This distinction appears in Tester,
Architect and Product documents. Native OS file-picker keyboard navigation
remains unverified, as it did in test6; supplying a file through the managed
browser API is not proof of operating the OS dialog.

The acceptance evidence covers exact task data after reload, mixed-case
title-or-notes search AND both filters, full-board export under filters, explicit
replacement and cancellation, malformed JSON, wrong schema, invalid records,
mixed valid/invalid records and duplicate IDs. Preservation cases compare saved
JSON, downloaded JSON and rendered fields. In-page keyboard and focus checks
exercise labelled controls and distinguish empty from no-match states.

A post-run source audit confirmed validation of the whole imported board before
replacement, persistence before changing in-memory state, text rendering via
`textContent`, and a static asset allowlist on the local server. This audit did
not modify the candidate or substitute for the agent tests. Test7 has eight
developer tests versus seven in test6, but that count alone does not establish
better quality. Test7's first candidate was worse on the observed filter
behavior; the accepted result meets the same scope. No broad claim of superior
usability, security or production readiness follows from this small experiment.

## Token and time comparison

Native CLI `token_usage_record` entries are deduplicated by response ID and
matched to each Env's exact scratch path. Main agents and `codex-auto-review`
are reported separately. Cached input is part of input; reasoning output is
part of output, not an additional chargeable token count. These are usage
measurements, not an invoice or a cross-model dollar estimate.

| Metric | Frozen test6 | test7 | Change |
| --- | ---: | ---: | ---: |
| Main input, including cached | 1,679,117 | 2,619,248 | +56.0% |
| Main cached input | 1,549,568 | 2,454,400 | +58.4% |
| Main uncached input | 129,549 | 164,848 | +27.2% |
| Approval uncached input | 112,385 | 134,071 | +19.3% |
| **Combined uncached input** | **241,934** | **298,919** | **+23.6%** |
| Main output | 23,371 | 28,045 | +20.0% |
| Approval output | 4,515 | 5,841 | +29.4% |
| Combined output | 27,886 | 33,886 | +21.5% |
| Main responses | 64 | 88 | +37.5% |
| Approval responses | 53 | 69 | +30.2% |
| MCP operations | 52 | 69 | +32.7% |
| MCP request bytes | 164,361 | 187,926 | +14.3% |
| MCP response bytes | 130,790 | 150,140 | +14.8% |
| Messages | 8 | 12 | |
| Runtime preflights | 1 | 1 | |
| Published source versions | 1 | 2 | Actual source correction |
| Browser requests | 2 | 3 | One test7 request rejected before execution |
| Completed browser runs | 2 | 2 | Full baseline, then focused recheck in test7 |
| End-to-end duration | 12m 01s | 14m 52s | +23.7% |

| Role | Main uncached test6 → test7 | Approval uncached test6 → test7 | Main responses test6 → test7 | Peak main context test6 → test7 |
| --- | ---: | ---: | ---: | ---: |
| Product | 38,892 → 20,892 | 16,874 → 15,534 | 12 → 11 | 27,186 → 25,922 |
| Architect | 20,581 → 33,298 | 22,508 → 31,268 | 16 → 24 | 32,988 → 40,705 |
| Coder | 27,257 → 60,049 | 36,545 → 41,946 | 18 → 25 | 38,532 → 42,447 |
| Tester | 42,819 → 50,609 | 36,458 → 45,323 | 18 → 28 | 34,699 → 38,446 |

Coder and Tester's post-baseline portions consumed respectively 29,016 and
13,010 additional uncached input tokens including approval review. Architect
also had extra consolidation and review work. These are observed phases, not a
counterfactual cost of the bug: the initial implementation and test design
differed too. Product became cheaper, but all technical roles became more
expensive. Initial role contexts remain approximately 17k tokens; the final
88 responses still exceed the soft 30–45 target.

Agent documents sampled 80 responses before final handoffs. The final native
CLI total is 88 and includes their subsequent responses. The frozen experiment
counters, rather than a mid-run handoff sample, are used throughout this table.

## What the latest fixes actually changed

| Area | Observation in test7 |
| --- | --- |
| Repeated JSON scenarios | Tester wrote one fixture/block-based specification. It repaired unsupported key values with `vfs_edit`, then reused the same spec for the source correction. No full-spec rewrite or duplicate recheck spec. |
| Focused browser verification | `only:["Combined search and radio filters, filtered export, clear"]` reran the affected case. Unlike test6's test-definition repair, this followed a real application correction. |
| Early validation | Unsupported `ArrowLeft` was rejected before Chrome startup. No partial run was credited as verification. |
| Keyboard download | The download assertion activated its control through Enter and checked the resulting file, including exact full-board contents under filters. |
| Preflight | Architect made one probe; the other roles reused the shared facts. No repeated capability investigation. |
| Result delivery | Architect's native CLI trace contains complete structured results for both Node runs and both Chrome runs, without repeating the expanded command. The baseline includes all ten case results and the failed assertion; the recheck includes its one case. |
| Artifact handoff | Browser evidence attached to the actual tested source version. The second artifact reflects a real two-file correction, not republishing unchanged source to move evidence. |

In test6 the expanded scenario occupied the detailed-result response and
Architect's read was cut off. Test7's compact result delivery fixed that observed
failure. The trace audit records parsable run details; missing objects are not
treated as proof that no manifest or summary was received.

Two MCP usability problems remain visible in test7. Tester requested
`ArrowLeft`/`ArrowRight`, which the runner does not support, and recovered using
the supported Up/Down radio navigation. It also attempted `workflow_status`
inside `env_read`, received “workflow_status is not a batch read”, and used
`env_call` instead. Both were recovered without human help. Natural horizontal
arrow support and consistent discovery/batching of read-only status operations
are concrete follow-ups; they were not silently fixed during this measurement.

## Role separation, documentation and limits of the comparison

Product stayed with criteria and the acceptance decision. Architect owned the
design boundary, code/evidence review and consolidated correction. Coder owned
implementation, developer tests and runnable instructions. Tester independently
authored the browser specification, preserved the failure and verified the fix.
All four updated their own documentation, and the final documents agree on
artifact IDs, correction scope and the OS-dialog limitation. The response-count
sample was the one stale operational detail, corrected in this experiment
report rather than rewriting the agents' frozen documents.

This is stronger evidence of working feedback than test6, which never needed an
application correction. It does not isolate the benefit of role separation:
there is no single-agent control, and both experiments already used four roles.
The larger technical contexts also show that separation does not eliminate
coordination or tool-context growth.

One run cannot establish a causal token-saving effect of the fixes. This run
demonstrates better use of focused checks and complete review evidence, together
with a higher total billable-work proxy. A future efficiency comparison should
separate first delivery from correction work and use multiple matched repeats;
it must not omit failing cases or weaken acceptance to meet a token target.

## Reproduction and archived evidence

Run the accepted app from `~/src/test7-queuepad/app` with `npm ci` and
`npm start`; open `http://127.0.0.1:3000`. `npm test` runs its developer suite.
Independent browser specifications are in `tests/queuepad.json`.

The local `evaluation/` archive contains `PROTOCOL.md`, the exact assignment,
launch/build hashes and instruction snapshots; `usage.json`,
`usage-summary.json` and `comparison.json`; messages and MCP logs; run records,
artifact manifests and final project hashes; the usage collector and native
trace audit. Final app/test/document hashes and evidence-to-artifact source
matches were checked after completion. The accepted source digest is
`5860a32e34b3e85c4063a7835cb4e7f07d9bc3e6fac78000cb1abba388cd7fbc`.

See [test6 review](AI_SPACES_TEST6_REVIEW.md) for the previous measurement and
the fixes made after it was frozen, and [workflow notes](AI_SPACES_WORKFLOW.md)
for the API and role contracts exercised here.
