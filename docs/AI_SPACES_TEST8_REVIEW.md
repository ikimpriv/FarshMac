# AI Spaces test8: browser keys, batched status and comparison with test7

Completed November 22, 2026, 12:49:14–13:00:33 America/New_York.

Four real FarshMac agents built QueuePad from scratch. Product accepted the
first immutable candidate after seven passing Node tests and ten passing
independent Chrome cases. Combined uncached input was **240,815 tokens**, down
**19.4%** from frozen test7. Elapsed time fell from **14m 52s to 11m 19s**.

This is an observational comparison. Test7 needed one real application
correction; test8 did not. The reduction cannot be attributed entirely to the
two API fixes, nor does one run establish that four roles outperform one agent.

## Controlled setup

- New Space `test8` (`ed626da3b97862d6`) and empty project
  `~/src/test8-queuepad`. No old application or tests were copied.
- Same QueuePad assignment, role instructions, VFS grants, contacts and
  directing rights as test7, with experiment names updated. Product delegates
  through Architect; Architect directs Coder and Tester.
- Product, Coder and Tester: `gpt-6.2-sol`, medium. Architect:
  `gpt-6.2-astra`, medium. Native CLI records confirm these profiles. All used
  Codex CLI `0.171.0`; Node `22.15.0`, npm `10.9.2`.
- One initial operator assignment. Afterwards only observation: no extra
  messages to workers, source edits, prompt changes or infrastructure changes
  during the run. The models were fixed, not adaptively selected.
- FarshMac was rebuilt and restarted before the run. The measured build added
  horizontal browser keys, `workflow_status` in `env_read`, and the host UI
  action for navigating from AI Spaces to a real file or folder. Build/source
  hashes are archived in `evaluation/build.json`.
- CLI profile-control improvements were made **after** the result and usage
  were frozen. They are not part of the measured test8 build.

## Result and evidence

Eight messages completed Product → Architect → Coder → Architect → Tester →
Architect → Product → user. Architect performed the single shared preflight,
reviewed immutable source, then read the complete verification results before
acceptance. No operator intervention or correction cycle was required.

Candidate: `32d4cb548cf29907`, ten source files, fingerprint
`31b7c4c330567d9e7b099c8f60fdc1de61005add7ddf139fc1768dc2691a8774`.

| Evidence | Outcome |
| --- | --- |
| Workspace `31af3ebe561c9e98` | 7/7 Node tests passed; source unchanged. |
| Chrome `56ff610eccab3fb2` | 10/10 cases, 514 expanded steps; source unchanged. |

There was one browser call and one reusable 31,774-byte JSON specification:
`/tests/queuepad.json`, SHA-256
`918e8daadf6be863a1c45adad13677a0afe269903b2a7b1365274e4a620fe14b`.
Fixtures and blocks were shared. The spec was not repeatedly regenerated or
repaired. Five workspace runs built the source in stages and verified it; only
the last matching run was attached as the candidate's developer-test evidence.

The cases cover CRUD and whitespace validation; all fields after real reload;
status movement; title-or-notes search intersected with priority and status;
resetting filters; complete export while filtered; explicit replacement and
cancellation; malformed JSON, invalid envelope, mixed-validity records and
duplicate IDs; full-board preservation immediately and after reload; keyboard
activation, focus restoration, labels and distinct empty states.

Both `ArrowLeft` and `ArrowRight` were used in two passing cases, including
native radio selection and resulting task data. All four roles called
`workflow_status` through `env_read` (five batches in total), with no errors.
There were no recorded MCP errors. These directly exercise the defects found
in test7, rather than only passing new unit tests.

Native CLI tool-output auditing confirmed Architect received complete compact
details for both attached runs, including all ten browser outcomes. There was
no repeated resolved browser command in those responses and no truncation of
those run details. Product received the consolidated disposition and reviewed
the role documents; it did not duplicate Architect's full technical review.

The system file picker and screen-reader announcements remain unverified.
File injection through the managed browser is not evidence of operating the
OS picker. Native select-popup limits did not affect this app, which uses radio
groups. Product, Architect and Tester all carried these limits into handoffs.

Post-run source inspection confirmed whole-file import validation before
replacement, storage writes before state changes, text rendering via
`textContent`, synchronous filter reset before rendering, and an allowlist for
local static assets. This was read-only and did not replace the agents' tests.
No broad production-readiness or comprehensive security claim is made.

Test7's accepted result was supported by nine unaffected baseline passes plus
one focused recheck after a two-file correction. Test8 ran all ten cases on its
accepted candidate. Functional scope is comparable; the initial test8 candidate
was better on the observed filter behavior. Test7 remains the stronger direct
demonstration of agents finding and repairing a defect together.

## Token, time and transport comparison

Counters come from native CLI `token_usage_record` entries, deduplicated by
response ID and matched to each Env's exact scratch directory. Main agents and
automatic approval reviews are separated. Cached input is included in input;
reasoning output is included in output. Root/setup/audit costs are excluded
from both runs. These are token measurements, not an invoice or dollar estimate.

| Metric | Frozen test7 | test8 | Change |
| --- | ---: | ---: | ---: |
| Main input, including cache | 2,619,248 | 1,524,133 | −41.8% |
| Main cached input | 2,454,400 | 1,385,984 | −43.5% |
| Main uncached input | 164,848 | 138,149 | −16.2% |
| Approval uncached input | 134,071 | 102,666 | −23.4% |
| **Combined uncached input** | **298,919** | **240,815** | **−19.4%** |
| Main output | 28,045 | 21,829 | −22.2% |
| Approval output | 5,841 | 4,093 | −29.9% |
| **Combined output** | **33,886** | **25,922** | **−23.5%** |
| Main responses | 88 | 57 | −35.2% |
| Approval responses / MCP calls | 69 | 47 | −31.9% |
| MCP request bytes | 187,926 | 131,949 | −29.8% |
| MCP response bytes | 150,140 | 130,072 | −13.4% |
| Messages | 12 | 8 | −33.3% |
| Elapsed | 14:52 | 11:19 | −23.9% |

Approval input including cache was 944,522 tokens (841,856 cached). Main
reasoning output was 2,146; approval reasoning output was 1,341, both already
included above. Product quoted 53 main responses during its acceptance review;
the final frozen total is 57, including the remaining handoff and final turns.

| Role | Main uncached, test7 → test8 | Approval uncached, test8 | Responses | Peak main context, test7 → test8 |
| --- | ---: | ---: | ---: | ---: |
| Product | 20,892 → 31,947 | 14,815 | 12 | 25,922 → 25,585 |
| Architect | 33,298 → 32,914 | 24,321 | 19 | 40,705 → 37,097 |
| Coder | 60,049 → 50,497 | 31,232 | 14 | 42,447 → 38,449 |
| Tester | 50,609 → 22,791 | 32,298 | 12 | 38,446 → 35,142 |

## Roles, documentation and remaining inefficiency

- **Context:** responsibilities remained separate. Product consumed the PRD
  and disposition; Architect reviewed source and test details; Coder owned
  implementation; Tester authored independent cases. No peer transcript dumps
  or unrelated projects were needed. Peak contexts fell, but initial contexts
  remain approximately 17–18k tokens per agent. This is substantial overhead
  for such a small application.
- **Documentation:** each role maintained one concise living record: Product
  3.4 KB, Architect 2.4 KB, Tester 3.6 KB, Coder README 2.9 KB. They record
  criteria, evidence IDs, setup, profiles and limitations without copying
  source into design documents. Product and Architect updated their records
  after review; Coder's baseline README remained accurate.
- **Feedback:** actual implementation and test evidence returned through the
  assigned review chain. Architect reviewed complete test outcomes; Product
  accepted with limitations. No correction feedback was necessary this time.
- **API efficiency:** one preflight, one spec, one browser execution, bounded
  batched reads and complete compact evidence worked. Twelve `env_info` calls
  still incurred schema-discovery overhead. Five construction/test workspace
  calls were sequential and could potentially be consolidated, but inspection
  did not show repeated full source rewrites.
- **Cost:** 57 main responses still exceeded the soft 30–45 target. Product's
  uncached input grew despite similar peak context, so fewer overall calls do
  not guarantee savings for every role. Approval reviews account for 42.6% of
  combined uncached input; their policy was unchanged.
- **Models:** configured profiles were applied, with no unsupported model
  names or changes. Because this repeat held them fixed, it cannot show that
  role-based automatic model/effort selection is optimal.

## Audit files and local use

The project and `evaluation/` directory contain frozen setup/prompts, build
hashes, assignment, messages, per-role MCP logs, native usage counters, run
records, artifact manifest, source hashes, trace audit and this report. The
comparison uses frozen test7 data, never later live counters.

From `~/src/test8-queuepad/app`:

```sh
npm ci --offline --no-audit --no-fund
npm test
npm start
```

Open `http://127.0.0.1:4173/`. There are no package dependencies; the lockfile
and documented Node/npm versions are included.
