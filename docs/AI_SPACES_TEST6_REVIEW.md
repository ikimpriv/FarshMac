# AI Spaces test6: workflow and token review

Completed November 21, 2026, 23:10:00–23:22:01 America/New_York.

The four real Codex agents completed the QueuePad task autonomously in 12m 01s.
Combined uncached input fell from 486,848 tokens in frozen test5 to 241,934 in
test6 (50.3%). Output fell 27.4%. The candidate passed seven Node checks and ten
independent managed-Chrome cases. Product accepted it for local use, explicitly
excluding verified keyboard navigation inside the operating-system file picker.

This is one matched observational repeat, not a causal estimate or a billing
statement. The implementation and test design can differ between runs.

## Controlled setup

- New Space `test6`, new empty project `~/src/test6-queuepad`.
- Same seven QueuePad criteria and exclusions as test5: CRUD, status movement,
  combined search/filtering, real reload persistence, complete export and atomic
  import, keyboard/labels/empty states, and reproducible plain HTML/CSS/JS on a
  local Node server. No old application or tests were copied.
- Same four roles, grants and contact graph. Product directs Architect;
  Architect coordinates Coder and Tester. Each owns its documentation binding.
- Product, Coder and Tester: `gpt-6.2-sol`, medium. Architect:
  `gpt-6.2-astra`, medium. Observed native CLI records match requested settings.
- One initial assignment to Product; no operator intervention, messages to
  workers, test execution on their behalf or collaboration sub-agents.
- Approval policies and sandbox boundaries were retained.
- Baseline is the archived test5 measurement, not its subsequently resumed live
  sessions. Root/setup/build/audit tokens are outside both experiment totals.

The project `evaluation/` directory contains the protocol, launch build hashes,
instruction snapshots, messages, MCP logs, run records, artifact manifests,
collector, per-response usage, comparison and final file hashes.

## Changes exercised by test6

1. Browser scenarios are written once to a VFS JSON specification and invoked by
   path. Named fixtures, external fixture files and reusable step blocks replace
   expanded payloads. Specifications are data, not executable code. Bounded
   automatic batches preserve independent browser contexts.
2. Browser assertions distinguish exact text from substring matching. Focus is
   checked explicitly; keyboard dispatch uses native codes and focus emulation.
   Unsupported native select-popup keyboard behavior is reported as a separate
   capability, without blocking supported browser tests or falsely passing it.
3. Runtime preflight is shared across Envs, coalesced and cached. Architect owns
   the probe; Product and workers reuse its result. Cached facts are available
   through `env_info` without another browser probe.
4. Browser evidence automatically attaches to the existing immutable artifact,
   including failed checks. Reviewers exchange that ID rather than republishing
   source or trying to attach another Env's private run. Tool help and errors
   explain the workspace/VFS/artifact distinction.

## Observed execution and evidence

Eight messages completed the full Product → Architect → Coder → Architect →
Tester → Architect → Product → user cycle. There was one preflight, one source
candidate and no implementation correction cycle. Architect read the code and
reviewed the Tester report and recorded evidence; Product recorded acceptance.
The absence of an application defect means this run did not exercise a
Tester → Coder correction loop.

The only artifact is `868e26de54efd762`, with source digest
`cfb3500a9aa824168542f4e911cb60475f766a32ca5e0c8d00a1d67b5c910e8d`.

| Evidence | Result |
| --- | --- |
| Workspace `3a7f4eec00188a0a` | Seven Node checks and syntax check passed; before/after fingerprints match the candidate. |
| Chrome `4026140b6140aebc` | Nine of ten cases passed. The export assertion omitted its required trigger selector. Failure retained. |
| Chrome `30bf60bb0bad8367` | Ten of ten cases passed against the same candidate after repairing the test definition. |

The final spec has 492 expanded steps, executed in three automatic batches.
It checks full-board JSON equality, filtered export, title/notes search AND both
filters, edit/move/delete persistence, explicit import confirmation, Cancel and
Escape, malformed JSON, wrong schema, invalid records, mixed valid/invalid
records and duplicate IDs. Browser cases have fresh storage and downloads.
Radio-group keyboard interaction was verified; native select popups were
avoided after the shared capability report. The OS file picker remains outside
the automated evidence. That limitation is recorded by Tester, Architect and
Product rather than silently treating a managed upload as OS-dialog navigation.

No top-level MCP operation failed. The first browser failure was an assertion
definition error reported in the structured run result. No agent republished
the artifact, retried a private run attachment or repeated the preflight.

## Usage comparison

Native CLI `token_usage_record` entries are deduplicated by response ID and
matched to each new Env's exact scratch directory. Main responses and
`codex-auto-review` responses are counted separately. Cached input is a subset
of input; reasoning output is a subset of output and is not added twice.

| Metric | Frozen test5 | test6 | Change |
| --- | ---: | ---: | ---: |
| Main input, including cached | 3,156,900 | 1,679,117 | −46.8% |
| Main uncached input | 236,452 | 129,549 | −45.2% |
| Approval uncached input | 250,396 | 112,385 | −55.1% |
| Combined uncached input | 486,848 | 241,934 | −50.3% |
| Combined output | 38,436 | 27,886 | −27.4% |
| Main responses | 99 | 64 | −35.4% |
| Approval responses | 84 | 53 | −36.9% |
| MCP operations | 79 | 52 | −34.2% |
| MCP request bytes | 397,477 | 164,361 | −58.6% |
| MCP response bytes | 170,656 | 130,790 | −23.4% |
| Runtime probes | 3 | 1 | |
| Published artifacts | 3 | 1 | |
| Browser requests | 9 (one rejected) | 2 | |
| End-to-end duration | 17m 44s | 12m 01s | −32.2% |

| Role | Main uncached test5 → test6 | Peak main context test5 → test6 | Main responses test5 → test6 |
| --- | ---: | ---: | ---: |
| Product | 37,000 → 38,892 | 26,722 → 27,186 | 12 → 12 |
| Architect | 28,535 → 20,581 | 40,629 → 32,988 | 24 → 16 |
| Coder | 45,935 → 27,257 | 34,793 → 38,532 | 16 → 18 |
| Tester | 124,982 → 42,819 | 53,915 → 34,699 | 47 → 18 |

The largest reduction is Tester's repeated scenario/tool-repair work. Its
approval peak also fell from 146,238 to 40,568 input tokens. Product's usage did
not improve, and Coder produced more output (11,886 → 13,549) over more responses.
Initial role contexts remain around 17k tokens. The result supports reducing
repeated payloads and coordination overhead, not a blanket claim that every
role or model became cheaper. No adaptive model/effort choice was tested.

## Documentation, feedback and remaining overhead

Product maintains one living PRD with criterion-level acceptance and the exact
run commands. Architect maintains a design/review disposition and observed
profile table. Coder supplies the README, schema and local test runner. Tester
owns a reusable specification and a criterion/evidence report, preserving its
initial failed check and explaining the repair. Role boundaries were respected.

The run still exceeded the soft 30–45 response target (64). Tester rewrote the
full spec for a small fix and repeated all ten cases. Architect's detailed run
read was capped because the expanded scenario preceded the actual result;
the manifest still supplied the recorded exit status and Tester supplied the
case report. This is a real review-delivery weakness, not evidence that the full
detailed result reached Architect. Product's final caveat was appropriately
more precise than an unqualified “all seven criteria verified.”

## Fixes added after freezing test6

These are covered by implementation tests but **are not credited with any
additional token savings in the table above**:

- Compact artifact run reads put status, provenance and result first, omitting
  the expanded scenario. Full input/output is separately paginated with byte
  offsets and explicit truncation metadata. A regression verifies the result
  survives `env_read` with a 320k-byte recorded scenario.
- Recorded resolved browser input is replayable without the external spec;
  the original spec path/hash is kept in the result. Previously the record
  combined a spec path and inline cases, which the replay validator rejects.
- Action validation rejects a missing download selector before Chrome starts.
  A download step can activate its control via Enter/Space and verify that exact
  download, rather than triggering a separate click.
- `only:[case names]` selects affected cases from the same reusable spec.
  Default roles now request small `vfs_edit` corrections and focused rechecks.
  Exact-fingerprint migration upgrades untouched defaults only; custom templates
  and existing Env snapshots, including test6, are preserved.

## Implementation validation

The full application check script with race detection passed before the measured
run. After the evidence/API follow-ups, the entire AI Spaces package passed with
race detection and native Chrome enabled (91.3 seconds), including sandbox and
artifact-permission tests, browser batching, replay provenance, compact evidence,
keyboard downloads, deliberate failures and cancellation. The final prompt
migration has a separate test for untouched defaults and preserved customization.
No model calls are made by these implementation tests.

The application is built with the persistent `FarshMac Local Development`
identity at `src/build/FarshMac.app`. The run's original build fingerprint and
the later build fingerprint are archived separately. No Git commit, push or
release is part of this task.
