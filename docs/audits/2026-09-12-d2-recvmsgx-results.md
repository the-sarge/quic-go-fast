# D2 macOS recvmsg_x receive-batching results — disposition: Fail → retire

Results for the precommitted [D2 adoption protocol](2026-09-12-d2-recvmsgx-protocol.md) (Slice D2 of the [Darwin batch plan](../adr/2026-09-11-darwin-batch-plan.md), Track D, program QGF-DP-2026-09). Raw collection: [2026-09-12-d2-recvmsgx-collection.jsonl](2026-09-12-d2-recvmsgx-collection.jsonl).

## Disposition

**Fail → retire**, by the protocol's mechanical rule, on two independent gates plus engagement:

- **Primary — receive syscalls per delivered datagram (engaged / disabled):** geometric mean **0.9049**, 95 % paired-bootstrap CI **[0.8967, 0.9115]**. The fail threshold is a point ratio > 0.90; the adoption gate (upper bound ≤ 0.75) is not approached.
- **Throughput (engaged / disabled):** geometric mean **0.6708**, 95 % CI **[0.6678, 0.6738]** — a ~33 % regression against the ≥ 0.95 noninferiority gate (fail threshold: point < 0.90).
- **Engagement:** mean **1.785 datagrams per delivering batch read** (< the 2.0 pass bound); batched fraction 100 % (every delivered datagram arrived via `recvmsg_x` while engaged).
- **Memory:** engaged median peak RSS 25.7 MB vs disabled 26.3 MB — within the +8 MiB budget (not a factor).
- **Preservation/qualification:** all 10 counted rounds of the disabled and unqualified cells reported zero batch reads, nonzero fallback reads, and completed correctly; every engaged cell qualified through the allowlist + receive self-check and delivered batched datagrams.

Per the slice's pre-accepted dispositions, retirement means the experiment PR closes unmerged and the tree never contains the experimental path. The D1 batch send is untouched.

## Fixed configuration as executed

- **Base:** `81639180` (D1-era default-branch HEAD). **Measured candidate:** `489b7812` — the implementation-branch head carrying the batch receive path and the precommitted protocol; the results and collection documents were added after collection, leaving the measured code byte-identical. Recorded deviation: the protocol names "the D2 PR head at collection time"; the PR was opened from the measured head after collection rather than before it.
- **Host:** Mac16,5 (Apple M4 Max, 16 cores, 128 GiB), macOS product 26.6.2 (build 25G83), Darwin kernel 25.6.0 arm64 — allowlist entry Darwin 25. **Toolchain:** go1.27.0 darwin/arm64, one binary per collection (`go test -c -tags d2bench`).
- **Collection:** 11 rounds × 3 cells (engaged / disabled / unqualified), round 0 discarded as warmup, cell order rotated by round, one fresh process per invocation, 256 MiB of 1071-byte records per transfer, D1 send batching engaged and asserted in every cell. No concurrent builds or agent tasks were started during collection. Load average after collection: 4.62 / 2.58 / 2.31 (the collection itself is the dominant contributor). Recorded deviation: the load-before line was lost to driver-output truncation; the paired same-round design carries the drift-absorption burden the protocol assigns it.
- ~195,800 datagrams were delivered to the measured receiver per cell invocation, identical across cells (byte-equal transfers).

## Why the path loses

The loopback bulk workload is **sender-bottlenecked**: the receiver outpaces the D1-batched sender, so 61.5 % of engaged read cycles first park on the poller (a visible zero-progress `recvmsg_x` attempt) and the average delivering read collects only ~1.8 of the 8 offered messages. At that shallow fill, `recvmsg_x`'s per-call cost — the kernel internalizes and externalizes the full `msghdr_x` array and runs its multi-message receive loop — exceeds the saving from ~10 % fewer receive syscalls, and the extra receive-path CPU slows the whole in-process transfer (throughput 144.8 vs 216.4 MB/s median). The loss is syscall-path cost, not memory or allocation: the engaged cell allocates less than the disabled cell (no per-read allocation was added) and stays inside the RSS budget. Deep fills of the kind that make the D1 **send** side profitable (7.99 packets per submission, queue-accumulated) have no receive-side analogue here: arrival clumping is bounded by the sender's pacing, and a receiver that keeps up drains the queue before it deepens.

These numbers characterize this loopback microbenchmark on this host, per the protocol's scope statement; they are not a universal claim about `recvmsg_x`. For this program's adoption question they are decisive: the predeclared minimum useful effect is not met and the noninferiority gate fails outright.

## Termination

The fixed collection ran once and is published in full; no samples were deleted, and no repeated collection, workload widening, or added platforms follow. The slice ends in the pre-accepted **retired** disposition: the experimental path never merges, this protocol/results pair is the committed record, and Track D completes with D1 adopted and D2 retired.
