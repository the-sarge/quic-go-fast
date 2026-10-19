# #808: one readiness and L6 campaign on the combined BBRv3 revision

**Date:** October 19, 2026, America/New_York. **Scope:** [#808](https://github.com/the-sarge/quic-go-fast/issues/808). Measurement only: no product code change, adoption, default-controller change, release, CHANGELOG change, paid resource, host-setting change or ledger change. Q2 ([#599](https://github.com/the-sarge/quic-go-fast/issues/599)) is not started here.

**Status: registered; no counted observation yet.** This record holds the campaign's decisions and, after each stage, its answer, the operator's advancement disposition and the requirements the campaign discharges. The stage records hold the registrations and results:

- [Readiness](../2026-10-19-bbr-808-readiness/README.md): slice 6's stage on the combined revision.
- [Side stage](../2026-10-19-bbr-808-gso-receive/README.md): #743's STREAM cost, the received-DATAGRAM discard counter, and #743 on BBRv3, on loopback.
- [L6](../2026-10-19-bbr-808-l6/README.md): #777's registration with #792's trace arm; runs only after an advancing disposition.

## Why one campaign

Every change to BBRv3's code makes a new revision, and each new revision needs its own readiness measurement and an operator advancement disposition before L6 or qualification (the [port advancement decision](https://github.com/the-sarge/quic-go-fast/blob/ebfd9339/docs/audits/2026-10-16-bbr-port-advancement-decision/README.md#d5--what-advancement-leads-to-l6-named-and-not-started), D5). On October 18, 2026, the operator chose to measure once, on the revision that has #742, #800 and #803, instead of separately for each; #743 was decided and merged first, and #824 followed. The revision is `main` at `e7eb3f10`, whose production code equals `42b9b5de`.

## Accepted decisions (before any data)

The agent presented one plan with eleven recommended decisions on October 18, 2026, and the operator answered "Approve all (Recommended)" at about 23:20 America/New_York. At about 23:50 the same evening, after a development pair, the operator amended D3 (below).

| # | Decision |
| --- | --- |
| D1 | **The revision:** `main` as of the registration commit, `e7eb3f10`. Later commits are not measured; a BBR or send/receive-path code change on `main` before the readiness run would have stopped the campaign for a decision. |
| D2 | **Readiness:** slice 6's stage unchanged except the candidate, with slice 6's values beside each cell as context; nothing pre-cleared; slice 7's exceptions do not carry over. |
| D3 | **Amended: no extra arm.** The plan pre-registered a conditional Reno-on-candidate arm with #743's batching switched off. A development pair then showed that the frozen fixture's managed packet-I/O lease never GSO-batches (one `sendmsg` per packet with and without the switch), so #742 and #743 cannot reach readiness or L6 and the arm could isolate nothing. The operator chose "Drop the arm (Recommended)": readiness runs slice 6's stage exactly, and a raised Reno-on-candidate cell goes to the disposition with #824 named as a known, unattributed contributor. |
| D4 | **The disposition stop:** after readiness the agent stops and gives the operator a plain summary with one recommended advancement disposition, checked against every standing rule and the cell's history in every readiness record; if it recommends an exception or override, one `ras consider` reviews it first. L6 runs only after an advancing disposition. |
| D5 | **L6:** #777's registration unchanged plus #792's trace arm in pairs 1–3, with #800's one-line overlay update; the trace never enters the classification. |
| D6 | **#800's requirement:** STREAM's displacement beside #777's and #792's, and the trace's per-packet application-limited marking, judged from `Delivery.Limited`, not qlog `limitation=`. |
| D7 | **#803's requirement:** ECN stays on if no BBRv3 flow stops marking ECT(0) in any usable L6 observation, from relay2's per-flow ECN counts. |
| D8 | **#743's STREAM cost and #824's counter:** a short loopback side stage in the readiness window on #743/#824's harness with a controller flag; readiness and L6 keep their plain frozen-fixture binaries and report DATAGRAM loss from the fixture's counts. |
| D9 | **The record:** on `main` by one docs-only pull request, four sibling directories; raw archives on a pinned branch off `main`, their checksums on `main`. |
| D10 | **Reviews:** one `ras consider` of the whole registration before any counted data, at most one replacement; no separate registration-acceptance stop (a finding that would change a decision here stops the campaign for one); after results, planit's bounded RAS review of the pull request. |
| D11 | **Running:** unattended night windows by systemd timer, after the wiremux yield handshake and with GridCast's GARM CI idle; contamination triggers one rerun and never decides an outcome. |

## What the campaign must discharge

| Requirement | Source | Where |
| --- | --- | --- |
| Readiness on the fixed revision and its advancement disposition | #800, #803 (moved to #808 by the operator), the port advancement rule | Readiness; the disposition below |
| L6 again, with STREAM's displacement compared against #777 and #792 | #800 | L6, D6 |
| ECN stays on through an L6-length BBRv3 run | #803 | L6, D7 |
| Re-check #743's open STREAM cost (0.979×) | #743's K3 | Side stage, K3 |
| `DatagramReceiveDropsV1` beside DATAGRAM loss in DATAGRAM and mixed arms | #824's comment on #808 | Side stage; DATAGRAM loss also in readiness and L6 |

## Known before any data

- **Coverage.** The readiness and L6 fixture cannot exercise #742's or #743's send-path changes (D3's amendment); only the side stage measures #743 on BBRv3. #800, #803 and #824 do reach the fixture.
- **The agent's expectations** (stated in the plan, not results):
  - readiness probably raises the nine memory and latency cells that slice 6 raised on the port, so advancing would again need explicit operator exceptions;
  - a Reno-on-candidate loopback DATAGRAM cell may cross its limit, with #824's deeper queue a known contributor;
  - L6 very likely stays adverse overall, because #792 found DATAGRAM's displacement to be draft-06's own behaviour on this path, which nothing merged changes; what L6 can show is whether STREAM improved with #800 and whether ECN now stays on.

## Answer

To be added after the readiness stage.

## Advancement disposition

To be added: the verdict under the unchanged condition, each raised cell, the agent's recommendation and the operator's decision.

## Discharges

To be added after the stages.
