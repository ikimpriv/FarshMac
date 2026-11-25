# AI Spaces test10: Product + Coder versus four roles

Completed November 22, 2026, 14:04:51–14:16:57 America/New_York.

Two real FarshMac Codex agents built QueuePad afresh. Product independently
found a keyboard Clear filters defect, Coder corrected it, and Product verified
the affected behavior. The accepted result uses **244,533 combined uncached input
tokens**, **12.9% less than test9**, despite one correction cycle. Elapsed time
was essentially identical: **12:06 versus 12:08**. Combined output increased
2.3%; total input including cache increased 5.5%. This is not a blanket reduction
in tokens or a monetary-cost estimate.

For this small task, two roles preserved independent acceptance and a working
feedback loop with fewer messages and MCP calls. They also produced larger
per-agent contexts. One run per topology cannot establish a general advantage
for larger projects or isolate the effect of role count from different generated
implementations, prompts, model mix, caching and correction work.

## Controlled setup

Both test9 and test10 used the same signed FarshMac executable, Codex CLI 0.171.0,
Node 22.15.0 and npm 10.9.2. The executable SHA-256 is
`6359f2bd9d6282822edadd063564dec82fa831ecf401ad37435b7e4a0620eaef`.
The application was restarted before each experiment to clear its in-process
runtime-fact cache. No approval or sandbox policies were weakened.

Each used a new Space, empty app/tests and the same seven acceptance criteria:
validated task CRUD; Todo/Doing/Done; title-or-notes search AND status AND priority
with Clear; real reload persistence; full export and atomic confirmed/cancelled
import; labelled keyboard controls and empty states; reproducible local plain
HTML/CSS/JS with Node and a lockfile. Neither could read previous projects.

| Responsibility | test9 | test10 |
| --- | --- | --- |
| Scope and acceptance | Product | Product |
| Design and implementation | Architect + Coder | Coder |
| Independent browser cases | Tester | Product |
| Review and correction coordination | Architect | Product |
| Model profiles | Product/Coder/Tester: sol medium; Architect: astra medium | Both: sol medium |

Full model IDs are `gpt-6.2-sol` and `gpt-6.2-astra`. Native records confirm all
observed main responses used the configured model and medium effort. No adaptive
model choice was tested.

Test10 prompts explicitly assign the missing roles' work instead of referring
agents to nonexistent Architect/Tester peers. Product can write `/product`,
`/tests` and `/acceptance`, but only read `/app` and `/architecture`. Coder has
the inverse write authority and cannot alter Product's acceptance cases. Product
may direct Coder, and both may communicate. No additional agents were created.

One operator assignment started each run. Subsequent operator activity only
observed logs and evidence. There were no source/test edits, worker instructions
or infrastructure fixes by the operator during either run. Both completed
without intervention. Frozen snapshots exclude root setup and auditing usage.

## Result and correction evidence

Baseline: `85303649be41ee1e`, fingerprint
`b9cf0c3ff7fda6ba6ddc6cbe3980552beacd424203af8b623c9e91988f6a72a5`.
Accepted candidate: `5ace075367fda260`, fingerprint
`d60dbb56b40b43e3a3ed93dd1fbbcaf5f18a23cbe722d37faeac86325eb3aae7`.
Both contain eleven files, excluding dependencies/caches.

| Run | Version | Outcome |
| --- | --- | --- |
| `279090b2d82764aa` | Baseline | Eight Node tests, offline lockfile installation and script syntax passed. |
| `cf208f82b6f7337f` | Baseline | 9/10 browser cases passed; keyboard Clear failed. 446 expanded steps. |
| `eb112d07c2caebfe` | Baseline | Focused diagnostic failed again: both radios reset to All, but no task appeared. 72 steps. |
| `ddc0aec7f3b5ed17` | Corrected | All eight Node tests, offline install and syntax passed. |
| `3ed4c7e0fb9d65cb` | Corrected | Both affected filter and keyboard cases passed. 113 steps. |

The six-message chain was assignment → Coder → baseline → correction → corrected
candidate → final user reply. Product retained the original failed case. Its one
spec edit added diagnostic assertions for the reset controls; it did not remove
or relax the failing visible-card assertion. After diagnosis, it sent one scoped
correction through the existing thread and retained Coder's conversation.

Only `app.js` and `index.html` differ between source artifacts. Clear was a native
reset button with rendering scheduled from the reset event via `queueMicrotask`.
Keyboard activation could render before the native reset default action, leaving
a stale board despite cleared controls. The fix uses an ordinary button whose
click handler calls `filters.reset()` and then `render()` synchronously. Both
pointer and Enter activation exercise it. This is the same class of defect seen
in test7, independently reproduced here without access to earlier code.

**The full browser suite was not rerun on the corrected source.** Acceptance
combines the unchanged baseline coverage with two passing affected rechecks,
supported by the two-file diff. It must not be described as ten tests executed
on the final artifact. Both failed baseline runs remain attached to that version.
The reusable JSON spec was updated once for diagnosis and reused unchanged for
the final recheck; both use spec hash
`93ecda4a7cd657e1a03c1039af344736e1ce29ef76d88f5e415761ab6e74629d`.

## Quality and verification boundaries

The browser evidence checks actual rendered task fields and status after reload,
intersecting filters, complete downloaded JSON with filters active, explicit
replacement and cancellation, and whole-board preservation for malformed JSON,
wrong schema, invalid priority, mixed-validity tasks and duplicate IDs. Invalid
import cases compare every original task's displayed fields before and after
reload and the full exported board afterwards. These are not just checks for
an error message. The eight developer tests additionally cover storage failures
and a real local HTTP server.

Both runs support the same functional scope. Test9's first delivery had no
observed defects; test10 required a correction. Test10 supplies stronger direct
evidence that **two roles can discover, communicate, fix and independently
retest a real defect**. Its radios passed real ArrowRight, Tab, Enter, Space and
Escape interactions. Test9's native selects were changed programmatically, so
popup keyboard behavior remained unverified. Test9 performed stronger explicit
storage-level transfer assertions and a separate strengthened transfer pass;
test10 validated the full visible and exported board without direct storage
assertions. Test counts alone do not rank the two implementations.

OS file-picker operation and screen-reader announcements remain unverified in
both experiments. Managed uploads set the input's files; they do not operate
the operating-system picker. Product test10 accepted all seven in-app criteria
and documented fixture uploads, but its final user reply did not emphasize this
OS-picker limit as clearly as test9. The report records that gap explicitly.
There is no claim of comprehensive accessibility, security or production readiness.

Post-run read-only source inspection confirmed validation of the entire backup
before replacement, storage-before-memory commits, textContent rendering and a
focus fallback for cards removed from the visible result. It also confirmed the
scoped correction. No extra operator test execution is mixed into agent evidence.

## Token, transport and time comparison

Native `token_usage_record` entries are matched to exact Env scratch directories
and deduplicated by response ID. Main and `codex-auto-review` totals are separate.
Cached input is included in input; reasoning output is included in output. Final
responses after handoff samples are included. The final test10 count is 58, not
the 53 sampled in Product's report. No pricing conversion is attempted.

| Metric | test8, four roles | test9, four roles | test10, two roles | test10 vs test9 |
| --- | ---: | ---: | ---: | ---: |
| Main input including cache | 1,524,133 | 1,783,612 | 1,878,019 | +5.3% |
| Main cached input | 1,385,984 | 1,646,080 | 1,757,184 | +6.7% |
| Main uncached input | 138,149 | 137,532 | 120,835 | −12.1% |
| Approval uncached input | 102,666 | 143,265 | 123,698 | −13.7% |
| **Combined uncached input** | **240,815** | **280,797** | **244,533** | **−12.9%** |
| Main output | 21,829 | 22,911 | 23,474 | +2.5% |
| Approval output | 4,093 | 4,485 | 4,541 | +1.2% |
| **Combined output** | **25,922** | **27,396** | **28,015** | **+2.3%** |
| Main responses | 57 | 66 | 58 | −12.1% |
| Approval responses / MCP operations | 47 | 67 | 50 | −25.4% |
| MCP request bytes | 131,949 | 171,821 | 132,165 | −23.1% |
| MCP response bytes | 130,072 | 172,006 | 109,179 | −36.5% |
| Messages | 8 | 8 | 6 | −25.0% |
| Elapsed to final reply | 11:19 | 12:08 | 12:06 | −0.3% |

Test10 approval input including cache was 1,565,490 (1,441,792 cached), compared
with 1,479,073 (1,335,808 cached) in test9. About half of combined uncached input
in each run belongs to automatic approval reviews. Fewer main turns alone is
therefore an incomplete efficiency measure. Compared with test8, test10 still
uses 1.5% more combined uncached input and 8.1% more output, with an actual
correction to perform. The observations do not establish a universal two-role win.

| test10 role | Main uncached | Approval uncached | Main responses | Peak main context |
| --- | ---: | ---: | ---: | ---: |
| Product | 58,674 | 45,438 | 31 | 41,687 |
| Coder | 62,161 | 78,260 | 27 | 43,321 |

## Responsibilities, documentation and efficiency

- **Context separation:** Product retained independent acceptance authority and
  read-only source access. Coder could not change Product's test assertions.
  Two initial contexts totalled 34,508 input tokens versus 69,231 across four
  roles in test9. This is context size, not additive uncached cost. Peak Product
  context grew from 29,742 to 41,687, and Coder from 36,105 to 43,321: fewer
  agents concentrate more responsibilities in each conversation.
- **Documentation:** Product maintained a 1,918-byte PRD and 2,653-byte acceptance
  report. Coder maintained a 3,090-byte design note and 945-byte README. Both
  updated the records after the correction, preserving version/run IDs and the
  failure. Test9's corresponding four records totalled 15,465 bytes versus 8,606
  here. More concise documentation still covered setup, decisions and evidence.
- **Feedback/review:** Product independently authored ten browser cases, found
  the real failure, diagnosed rather than weakened the assertion, and reviewed
  the affected checks on the corrected version. Coder changed only two source
  files. There was no distinct architectural reviewer; this experiment cannot
  establish whether that omission is safe for a more complex system.
- **MCP and code work:** no MCP protocol/API errors, one preflight, one browser
  spec and targeted rechecks. Coder used a pulled workspace, seven workspace
  operations including construction/correction/tests, and two pushes. Product
  avoided test9's six paged reads of expanded browser commands. Nine schema
  discovery calls remain, six from Coder across its two turns; schema knowledge
  is still rediscovered. Browser failures were successful tool invocations that
  returned failed evidence, not transport/API failures.
- **Model choice:** profiles stayed fixed. This comparison tests role topology
  plus its prompt/access changes and model mix; it does not evaluate automatic
  effort/model selection or price-optimal routing.

For a small, bounded application, Product + Coder is a credible default to test
further: independent acceptance survived and coordination costs fell. The evidence
supports retaining a separate Architect or Tester when risk, scope or genuinely
parallel work warrants it, not creating four roles mechanically for every task.
Repeated runs on several task sizes are needed before changing global defaults.

## Archived artifacts and local use

Projects and Spaces remain available as `test9` and `test10`.
`~/src/test10-queuepad/evaluation` holds frozen setup, prompts, assignment,
build/source hashes, messages, MCP logs, native usage, ten run records, both
artifact manifests, the comparison and trace audit. The source and independent
browser spec remain in their separate app/tests folders. See also
[the detailed test9 report](AI_SPACES_TEST9_REVIEW.md).

From `~/src/test10-queuepad/app`, run `npm ci`, `npm test`, then
`npm start` and open `http://127.0.0.1:4173/`. There are no third-party packages.
