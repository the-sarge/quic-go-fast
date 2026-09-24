# T3 control-admission scoped re-audit

The [current T3 contract](../../adr/2026-09-24-bbrv3-transport-plan.md#t3) is normative. This receipt records why retained [PR #604](https://github.com/the-sarge/quic-go-fast/pull/604) paused and how its same-child execution resumes. It is not another copy of the slice contract.

## Trigger and precise-root disposition

The initial review `20260924T061208-f69afaf4e7e4469ab49ccbba` examined `06a6af65a48286c831383a52dd948500beb63069`. Its accepted ordinary-migration control refusal and MTU pacing findings were fixed and verified at `049b68bfaae8a82ed9bc78f0eb245ca50049ab19`. A test-observer race was removed without changing production ownership. [Initial dispositions](https://github.com/the-sarge/quic-go-fast/pull/604#issuecomment-5808954522), [fix receipt](https://github.com/the-sarge/quic-go-fast/pull/604#issuecomment-5809015797), [race diagnosis](https://github.com/the-sarge/quic-go-fast/pull/604#issuecomment-5809068619), and [verification receipt](https://github.com/the-sarge/quic-go-fast/pull/604#issuecomment-5809121369) retain the details.

Replacement review `20260924T064126-61b11a739e383ce30014c4a4` found a due-MTU isolation request returning a hard block while ordinary bytes were pending and bounded control credit remained. C-002/C-003 corroborate C-001; C-004’s claimed ACK pacing debit is rejected because that ACK uses a nonordinary reservation. The [routing receipt](https://github.com/the-sarge/quic-go-fast/pull/604#issuecomment-5809319604) freezes product editing/review/certification while this revision is published.

| Comparison | Initial accepted family | Replacement observation |
| --- | --- | --- |
| Exact previously recorded obligation | Preserve control during ordinary-only migration debt | Preserve control while a due probe cannot acquire isolation |
| Concrete guard | Ordinary reservation refusal and its connection timer-result consumer | Isolated reservation refusal on nonzero pending bytes |
| Semantic distinction | Ordinary admission is barred by generation debt | Isolation acquisition is barred even by current-generation ordinary bytes |
| Earlier fix coverage | `waitForOrdinary` handled the ordinary refusal family | The isolated-refusal branch bypassed that helper |

Calling these identical precise roots would broaden the earlier recorded family. The replacement nevertheless demonstrates an unmet accepted control exception after the fresh-review budget is consumed, so the disposition is `stop-for-decision` routed through the invocation-authorized same-child re-audit. Shared package names or the broad word “admission” alone do not establish root identity.

## Gate results and retained boundary

The central repair is one ledger admissibility predicate shared by reserve and rearm, plus one emission control-opportunity disposition for a failed noncontrol request. The finite supported request census is ordinary data, control ACK/PTO, and isolated MTU probe; conditions that change behavior are available byte credit, old-generation debt, nonzero pending bytes, active isolation and physical queue slots. Probe pacing remains the already-verified separate token/debt owner. Failed isolation is distinct from an already-held isolated reservation: only the latter excludes all queued traffic. Isolation acquisition requires zero total tracked pending bytes, including control. A due probe suspends ordinary admission while that debt drains, but bounded control remains eligible; completion has no time bound amid ongoing worker or control activity.

Representation remains factory-produced typed requests/entries and existing send-result semantics, with universal enforcement by those owners and finite regression evidence. Runtime behavior and safety guards remain product deliverables; the existing tests are verification aids, with no new maintained harness. No parser, public selector, worker protocol mutation, raw-socket authority, persisted schema, offload capability or adjacent slice is introduced.

Ownership and authority are complete through admission, packing, registration, handoff, completion/disposal, wakeup and connection timer consumption. The existing private-policy transitional seam is retained; no parallel mutable owner is added. The traced blast radius includes ordinary waits, waiting probe isolation, active isolation, ACK/PTO timers and wakeup rearming. No new untraced effect is accepted. The normative matrix owns current coverage and uncovered cells.

Splitting the repair would leave reservation authority and its timer consumer disagreeing; merging a neighboring transport/controller slice adds no necessary behavior. Retain child #589 and the one product PR. Ten test functions remain the ceiling: extend the existing MTU case with due-ACK and no-ACK pending-work cases, retain migration/timer coverage, observe hard-block emission dispositions for active isolation and insufficient control credit, cover current-generation ordinary GSO refusal when one control packet still fits, and show isolation admission after pending bytes retire. No mutation, platform, timing-boundary campaign or repetition scope is added. The helper/consumer ranges and two named test functions supplement the normative T3 input inventory, common contract and named design sections. Measure the complete resolved additive manifest before product resume against the 35,000-token dispatch ceiling; this receipt replaces complete review reports within the 3,000-token history allowance. No unmeasured total is claimed.

## Resume and termination

Publish this documentation revision on main before updating the child’s exact plan pointer. Keep parent/program/OmniFocus mirrors to current state and pointers. Reconcile retained PR #604 with that main revision, make the new MTU/control case red, implement the shared predicate/disposition, run the admitted checks, and verify the replacement review’s accepted finding at the exact pushed head. There is no third fresh product review. Another counterexample escaping the shared predicate requires an operator decision. Product PR #604 continues to own T3 completion and successor-frontier changes; journal and task closure follow its merge.
