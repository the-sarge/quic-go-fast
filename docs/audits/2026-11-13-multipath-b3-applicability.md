# B3 evidence applicability record

**Scope:** [B3 #1042](https://github.com/the-sarge/quic-go-fast/issues/1042) records which Reno and native evidence still applies after the B track's source changes, and which changed surfaces need requalification, as the [program budget and prior-evidence validity](../adr/2026-10-24-multipath-bbr-plan.md#program-budget-and-prior-evidence-validity) section requires. This is a source-based record, not a measurement. It adds no campaign, rerun, gate or budget, and spends nothing from the 40-run reserve.

The baseline is the R19 #1039 delivered candidate `d7854ed7276a046ba06a33b9fe6e24203e0d555e`, against which the [native parent closure note](2026-11-12-native-parent-closure.md) already carried the E6, R21, R17 and R18 evidence. Since then, the production changes are B1 #1040 (PR #1327), B2 #1041 (PR #1331), the #1329 fix (PR #1333), the #1330 fix (PR #1337), and B3's own PR.

## Changed surfaces and their reach

| Surface | Change | Reach outside BBR multipath |
| --- | --- | --- |
| `internal/ackhandler` path contexts, delivery sampler, recovery and ECN ledgers (B1, B2, #1330) | Per-path BBR contexts, shared aggregate ceilings and one shared feedback scratch | None. Each new branch requires a BBR root (`congestionEvents != nil`), and `AddPathWithRTT` keeps refusing a rich root. |
| `receivedAck` carrier argument (B1) | Excludes an ACK carried on another path from a rich context's timing | None. The exclusion requires `congestionEvents != nil`; Reno contexts take the unchanged branch. The moved `MaybeExitSlowStart` call stays on that branch. |
| `BBRSender.Feedback` aggregation (#1329) | An event without a timing-eligible anchor accrues no aggregation | Single-path BBR changes, by owner decision; Reno doesn't. Single-path BBRv3 remains experimental and unqualified while Q2 #599 is deferred. |
| `handleAckFrame` (B3) | A multipath connection passes the carrier path through `ReceivedPathAck` | An ordinary connection still calls `ReceivedAck`. A Reno multipath connection now passes the carrier, which its contexts ignore, as in the row above. |
| `admitMember` and `releaseMember` (B3) | BBR installs and reclaims through `addBBRPath`/`removeBBRPath` | None. The Reno branch still calls `AddPathWithRTT` and `RemovePath`. |
| `enableMultipathCredit` (B3) | The private guard no longer refuses BBR | None. The public gate `admitMultipath` still refuses BBR multipath until B6. |
| `sendBounded` and the supply gap (B3) | BBR emission takes Reno's path turns, then the selected route's pacing, credit and supply gap | None. Only `e.bbr != nil` reaches `sendBounded`. A single-path BBR connection has no `pathCredit` and a nil route, so it keeps the root policy and its former order. |
| `commitTuple` (B2) | Pairs `resetBBRPath` with `MigratedPath` | A no-op without a BBR path policy. |

The native writers, endpoint admission, response epochs and stateless paths are unchanged since `d7854ed72`.

## Disposition

- **Reno primary/backup:** the R17 #1037 independent-peer evidence, the R18 #1038 ordinary-cost delivery receipt with its two owner-accepted CPU exceptions, and the E6/R21 native evidence still apply. Each changed surface above either leaves Reno and ordinary connections' behavior identical or isn't reached by them. Every non-documentation PR still runs the required independent multipath peer job on its exact head.
- **BBR primary/backup:** no earlier receipt applies. Its native, interop and ordinary-cost evidence is the planned B5 revalidation: the six interop instances and the twelve ordinary observations in the [candidate and run ledger](../adr/2026-10-24-multipath-bbr-plan.md#candidate-and-run-ledger). B3 adds no cell to that inventory.
- **Single-path BBR:** outside this program's qualification. The #1329 behavior change is recorded here only so later receipts don't treat earlier single-path BBR measurements as current.

No changed surface requires a new semantic cell or a new budget, so the B3 stop condition is not met.
