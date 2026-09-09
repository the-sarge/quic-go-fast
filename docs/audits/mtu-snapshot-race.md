# MTU fixture snapshot race

PR #145's initial head `ad534130d6387c5ffc0f08bbafe88e73405cc2cf` failed `TestPathMTUDiscovery` on Ubuntu, Go 1.27.x, QUIC v2 in [job 102661555478](https://github.com/the-sarge/quic-go-fast/actions/runs/34409872098/job/102661555478). Packet-loss tests passed in that job. The fixture reported MTU 1400, initial DATAGRAM limit 1163, final limit 1359 and server packet size 1234. Its final assertion required 1359 >= 1360. No CI rerun was requested; later passing checks on a changed head were not treated as diagnosis.

## Cause and evidence

The fixture sampled `SendDatagram` before reading the MTU event history while the connection was still processing packets. These are independently synchronized observations, not one snapshot. `mtuFinderAckHandler.OnAcked` records `MTUUpdated` inside `sentPacketHandler.ReceivedAck`. Only after that call returns does `Conn.handleAckFrame` publish the new `maxPayloadSizeEstimate`. Thus merely reversing the reads would still admit an event whose corresponding limit has not been published. A probe can also finish entirely between the old limit sample and the later event sample.

The captured 1359 equals the conservative estimate for MTU 1396: 1396 minus one header byte, the maximum 20-byte connection ID and the 16-byte authentication tag. An MTU of 1400 instead gives 1363, which satisfies the unchanged 40-byte tolerance. The job log does not distinguish the two observation windows above; it contains neither the precise limit-load time nor the ACK publication time.

`TestPathMTUDiscoverySnapshot` establishes the reachable publication window using the real UDP transport. Its typed qlog recorder pauses the first successful MTU probe's ACK after recording its scalar MTU value. An oversized `SendDatagram` call then observes the preceding estimate. The initial experiment used the fixture's consistency assertion during that pause and failed deterministically:

```text
MTU event=1326, DATAGRAM limit during ACK=1163
Error: "1163" is not greater than or equal to "1286"
```

The maintained regression asserts that this intermediate observation is stale, releases the ACK and closes the connection before comparing final values. Its control reports:

```text
after close: MTU=1326, DATAGRAM limit=1289
```

`CloseWithError` waits for the connection context to finish, which happens after the connection run loop and its current ACK processing return. The fixture now performs this existing teardown before collecting its final measurements. `SendDatagram` validates an oversized payload before reaching the closed queue, so it still returns `DatagramTooLargeError` and the final estimate. The regression covers that use after close explicitly. No new DATAGRAM is enqueued.

This fixes both a later probe overtaking the limit sample and an observation inside the publication window. It does not require changing production MTU calculation, event ordering, probing, loss policy, the existing 20-second transfer timeout or the 40-byte assertion tolerance. A focused race run also passed both the controlled regression and the original MTU fixture. The controlled test uses the first successful probe rather than reproducing the CI scheduler or insisting on a specific final probe size.

## Accepted scope and evidence ceiling

The operator was asked: “May I investigate and repair the separate MTU test snapshot-ordering failure, then update and revalidate PR #145?” and replied “ok”. This explicitly expands that PR to include this separate fixture repair. The MTU change is developed in its own worktree and commit before integration into PR #145; the #46 worktree and PR #144 remain independent.

The representation owner is the MTU integration fixture. The guarantee is a stable final comparison on a closed, single-path connection, with a controlled example of an ACK paused during MTU publication. Material artifacts are test fixtures, the regression recorder and this diagnostic report. The named regression and original fixture discharge the MTU criterion; arbitrary migration histories, runtime API changes, probabilistic campaigns and recursive testing of the recorder are outside this scope.

Validation consists of a failing intermediate-observation experiment, a passing final-observation control, focused v1/v2 and race runs, final full local certification and the applicable hosted matrix at the integrated PR head. The approved scope expansion gets a new bounded Standards/Spec and initial RAS review, verification of any accepted fixes, and at most one replacement review. The packet-loss review history remains evidence: initial run `20260909T215942-8749434025f1427e55fddebb` accepted the missing receive-trace synchronization; verification and replacement run `20260909T221248-e46db7e554293d86e0c3a367` cleared it at `3734105089cb002c2896e4f73bac2786f2b8e834`. This addendum does not reopen or strengthen the packet-loss contract.

```sh
go test ./integrationtests/self -run '^TestPathMTUDiscovery(Snapshot)?$' -version=1 -count=1 -v
go test ./integrationtests/self -run '^TestPathMTUDiscovery(Snapshot)?$' -version=2 -count=1 -v
go test -race ./integrationtests/self -run '^TestPathMTUDiscovery(Snapshot)?$' -version=2 -count=1 -v
```
