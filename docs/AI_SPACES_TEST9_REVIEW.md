# AI Spaces test9: four-role repeat on the current build

Completed November 22, 2026, 13:51:27–14:03:35 America/New_York.

Four real FarshMac Codex agents built a new QueuePad. The first delivered
candidate passed five Node tests, nine independent browser cases and six
strengthened transfer rechecks. Product accepted criteria 1–5 and 7; keyboard
verification remained partial because native select popup and OS file-picker
operation were unsupported. No operator intervention or post-delivery
application correction was needed.

Combined uncached input was **280,797 tokens**, 16.6% above frozen test8.
Main uncached input was essentially unchanged; automatic approval-review input
increased. The measured build includes the post-test8 CLI-control changes and
the role-creation UI, but this fixed-profile run does not isolate their effect.

## Setup and measurement

- New Space `test9`, empty project `~/src/test9-queuepad`.
- Exact test8 assignment and instruction snapshots, changing experiment and
  baseline references only. Same VFS grants, contacts and directing rights.
- Product, Coder and Tester: `gpt-6.2-sol`, medium; Architect:
  `gpt-6.2-astra`, medium. Native usage records confirm these profiles.
- Codex CLI 0.171.0; Node 22.15.0 and npm 10.9.2. No extra agents.
- Signed executable SHA-256:
  `6359f2bd9d6282822edadd063564dec82fa831ecf401ad37435b7e4a0620eaef`.
  The same executable is used for test10. FarshMac was restarted before each
  run to reset its in-process runtime-fact cache.
- One initial operator assignment, then read-only observation. No worker
  instructions, application/test edits or infrastructure changes during the run.

Native `token_usage_record` entries were matched to exact Env scratch paths and
deduplicated by response ID. Main and approval costs are separated. Cached input
is part of input; reasoning output is part of output. Root setup and audit usage
are excluded. These are token counts, not an invoice. The final freeze includes
Product's final response, whereas its acceptance document sampled an earlier
60 responses. The frozen total is 66.

## Evidence and quality

Candidate `6a8a8c5e966a6eb7`, nine files, source fingerprint
`6343724df20267b1c22bf1d4fba3649231442e2c6f6d9296b94bf71b3b06d305`.

| Evidence | Result |
| --- | --- |
| Developer `726385ae925073a8` | Five Node tests passed before Coder's pre-delivery focus correction. |
| Developer `0992f479967c6feb` | Five Node tests passed on the delivered candidate; attached to it. |
| Browser `941b178f861d4eec` | 9/9 cases, 409 expanded steps, on the delivered candidate. |
| Browser `898e1cafd685fdd7` | 6/6 strengthened transfer cases, 256 expanded steps, on the same candidate. |

Coder spotted an imported-ID focus lookup edge case after its first successful
Node run and fixed it before publication. This was not a reviewer-triggered
correction. Tester independently authored one reusable JSON specification with
shared fixtures/blocks, then made two targeted replacements to strengthen it:
full downloaded-board comparisons after reload and direct use of Cancel. It
reran only the six affected transfer cases. Neither browser run failed.

Coverage includes CRUD, trimmed/rejected titles, optional notes, all priorities
and statuses, AND filters, both search fields, Clear filters, durable edits and
deletions, filtered full-board export, confirmed/cancelled replacement, malformed
JSON, wrong schema, mixed valid/invalid tasks, duplicates and incomplete tasks.
Transfer cases check the whole stored and downloaded board before and after
reload. Labels, focus, traversal, button actions and empty states were tested.
Native select-popup keyboard changes and OS picker operation were explicitly
left unverified, not counted as passes. Test8's radio-based UI avoided the
select-popup gap, so test9 is weaker on this particular keyboard evidence.

Architect read complete compact results for the Node run and both browser runs,
with no repeated command payload or truncation in those run-detail objects.
Product reviewed the consolidated report and also inspected sections of the
expanded transfer commands. Native trace audit confirms the received run details.

Post-run source inspection found complete import validation before confirmation,
storage writes before in-memory replacement, textContent rendering, a static
asset allowlist and a focus fallback when a filtered card is no longer visible.
This read-only inspection does not establish comprehensive security, accessibility
or production readiness, and it did not replace agent verification.

## Token and time comparison

| Metric | test8 | test9 | Change |
| --- | ---: | ---: | ---: |
| Main input including cache | 1,524,133 | 1,783,612 | +17.0% |
| Main cached input | 1,385,984 | 1,646,080 | +18.8% |
| Main uncached input | 138,149 | 137,532 | −0.4% |
| Approval uncached input | 102,666 | 143,265 | +39.5% |
| **Combined uncached input** | **240,815** | **280,797** | **+16.6%** |
| Main output | 21,829 | 22,911 | +5.0% |
| Approval output | 4,093 | 4,485 | +9.6% |
| **Combined output** | **25,922** | **27,396** | **+5.7%** |
| Main responses | 57 | 66 | +15.8% |
| Approval responses / MCP operations | 47 | 67 | +42.6% |
| MCP request bytes | 131,949 | 171,821 | +30.2% |
| MCP response bytes | 130,072 | 172,006 | +32.2% |
| Messages | 8 | 8 | unchanged |
| Elapsed to final reply | 11:19 | 12:08 | +7.2% |

| Role | Main uncached | Approval uncached | Main responses | Peak main context |
| --- | ---: | ---: | ---: | ---: |
| Product | 40,621 | 35,282 | 14 | 29,742 |
| Architect | 44,030 | 25,367 | 18 | 39,362 |
| Coder | 28,261 | 47,877 | 18 | 36,105 |
| Tester | 24,620 | 34,739 | 16 | 35,957 |

## Collaboration and remaining waste

The eight-message chain completed without operator help. Each role maintained
its own documentation: PRD/disposition, design/review, README and acceptance
report. Tester supplied independently authored evidence; Architect reviewed it;
Product preserved limitations. This validates the delivery/review chain, but
without a failed candidate it does not exercise defect-feedback repair.

There was one preflight and no MCP errors. Schema discovery still used 13 calls.
Coder made nine individual VFS writes and two VFS edits rather than constructing
the source in fewer workspace operations, causing more approval reviews. The
second developer run was justified by its actual focus fix.

Product read about 24 KiB of expanded browser commands in six `artifact_read`
calls after Architect's review, overlapping technical evidence inspection. It
requested 8 KiB initially but received a 4 KiB page; subsequent offsets were
out of sequence before gaps were filled. This did not break acceptance, but
shows how a nominally lightweight Product role can accumulate review overhead.
Tester also strengthened already-passing tests in a second pass; the added
coverage was useful, although designing it into the first spec would avoid the
extra handoff-stage work. None of these costs demonstrates an API regression.

The experiment preserves same-scope evidence but does not show that four roles
are inherently better or cheaper. The paired two-role observation is recorded
in [the test10 comparison](AI_SPACES_TEST10_REVIEW.md).

## Files and local use

`~/src/test9-queuepad/evaluation` contains frozen prompts, setup,
assignment, build hashes, native usage, messages, MCP logs, four run records,
artifact manifest and project hashes. The project itself contains the source,
independent browser JSON and role documents.

From `~/src/test9-queuepad/app`, run `npm ci`, `npm test`, then
`npm start`; open `http://127.0.0.1:3000/`. There are no package dependencies.
