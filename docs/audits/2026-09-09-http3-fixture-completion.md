# HTTP/3 fixture completion audit — 2026-09-09

## Scope and trigger

This scoped architecture-handoff adds H3 to QGF-AD-2026-09/H. It retains the completed H1/H2 product work, the existing H parent #86 and program #101, and the pending append-only H2 journal #139. The current H3 contract is in [the H plan](../adr/2026-09-08-http3-lifetime-plan.md#h3--synchronize-http3-fixture-completion-assertions); this artifact records evidence and the design audit, not another contract.

The [journal failure receipt](https://github.com/the-sarge/quic-go-fast/pull/139#issuecomment-5604485821) records the macOS / Go 1.27 assertion failure in `TestExchangeActiveMultiplexedResponses` at `http3/exchange_test.go:114`: expected zero pooled uses after response EOF, observed one. The failed [hosted job](https://github.com/the-sarge/quic-go-fast/actions/runs/34370486565/job/102530145576) ran at journal head `2ce7f4b150917a1e8d8392bef71af4bc5c7cab6f`. Its HTTP/3 tree is identical to H2's certified head `7fbfd1b576b98498b7a1dd090d636554f3916199`; H2 merged as `9018906dc1e03782426818b96444d63e15019384`. No failed-head rerun or runtime/test edit was performed during this handoff.

## Verified design facts

Code inspection is pinned to `9018906dc1e03782426818b96444d63e15019384`.

- `http3/exchange.go:71–75` performs pooled release before closing the attempt's `stopped` channel. `responseFinished` at 107–115 waits only if both response and upload halves have finished; returning earlier while upload remains active is accepted H1 behavior.
- `http3/client.go:430–438` sends non-nil request bodies asynchronously. Uploader defers close the stream before reporting `uploadFinished`, so server-observed request EOF does not establish final owner cleanup.
- Local Go 1.27 standard-library inspection confirms `httptest.NewRequestWithContext` creates a server request through `http.ReadRequest`; zero-length transfer parsing uses `http.NoBody`. Supplying a nil reader does not replace that non-nil Body. The fixtures therefore admit an asynchronous empty upload. The standard library owns the representation; the handoff does not create a parser or canonicalization obligation.
- Six terminal count sites lack the complete-attempt join: multiplexed Close/EOF at `exchange_test.go:102,114`; gzip error/decoded EOF at 271,281; successful retry at 370; and response read error at 498.
- `TestExchangeActualBodyCompletion` uses `http.NewRequest(..., nil)` at 313; its synchronous setup upload finishes before a response is returned. Existing explicit joins at 166,199,236,440,473 already observe complete lifetimes. Acquired-dial/setup failures and pre-response failure have their own existing evidence. Those cases do not need blanket rewrites.

The observation error is sufficient to explain a valid schedule with count one after response EOF; this is a bounded code argument, not a claim to have reproduced the hosted scheduler or exhaustively ruled out unrelated H1 defects. A pooled count that differs from the expected remaining-active count after the relevant attempt's `stopped`, or an unreachable completion barrier, is an explicit implementation stop.

## Decisions and independent slice audit

The primary agent self-grilled the observation boundary, representation, owner, affected assertion families, preservation risks, and split/merge alternatives. A read-only independent agent audited the same pinned code and accepted one test-only H3 slice. Its two refinements are incorporated: include the first multiplexed response Close boundary as well as final EOF, and preserve true nil-body status fixtures unchanged. The independent draft audit passed after clarifying that a completed attempt leaves the expected count of other active attempts; nonzero total count alone is not an error. The final arithmetic is fifteen total slices, two implemented, thirteen remaining: nine frontier and four blocked.

| Gate | Disposition |
| --- | --- |
| Single owner and authority completeness | Keep `requestLifetime` and observe `stopped`; no mutation owner or persisted authority is added. |
| Representation and artifact classification | Internal Go fixture/channel domain; example-level coverage. Existing tests are verification aids; no maintained framework or parser is added. |
| Transitional seams | None; no product hook, state machine, generic polling helper or successor-dependent adapter. |
| Closure and evidence | Closure not triggered for this finite test observation repair. Six source sites and one controlled delayed-empty-upload subcase terminate the evidence. |
| Preservation and blast radius | Keep active-count assertions before completion; join only the terminated attempt. Preserve gzip, errors, retry input, idle-close and existing joined cases. No required untraced effect remains. |
| Context fit | One test file plus the existing owner/upload plumbing fits the 12k dispatch budget and one test-only product PR. |
| Further split | Rejected: fixing only the observed EOF leaves the same concrete observation error at five nearby sites. |
| Adjacent merge | Rejected: runtime normalization/completion changes would alter H1 semantics; folding tests into #139 would violate its append-only boundary. |
| Blocking edges | H3 has no open blocker because H1/H2 are merged. Pending H2 journal reconciliation follows H3's merged correction; unrelated frontiers are unchanged. |

No repeated-root stop is asserted. The two H2 review findings concern a program-count sentence and an optional helper comment; neither shares this exact fixture observation invariant and owner. Broad overlap with H1 lifetime terminology does not establish identity with any historical finding.

## Existing-work and resume disposition

Keep #139's existing journal bytes and clean feature branch. After the separately dispatched H3 correction merges, integrate the new default branch into #139 without losing or rewriting its entry, certify the resulting append-only diff, obtain green applicable checks on its new head, and merge it. Then finish H2's exact OmniFocus task and update H/program closure state. H3 retains its own post-merge journal and task ritual. The product/frontier transitions belong in product PRs; no routine third frontier PR is introduced.

This handoff publishes only the audited plan and pointer surfaces. It does not implement or dispatch H3, rerun failed CI, rewrite H1/H2 production code, or edit the pending journal.
