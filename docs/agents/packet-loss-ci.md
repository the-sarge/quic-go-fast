# Deterministic packet-loss CI

The operator approved the following proposal after the [PR #144 capture was diagnosed](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/issue-44-captured-loss/README.md): “make random-loss stress opt-in and use a documented deterministic loss corpus in mandatory CI, including this timeout case and successful recovery controls”. The accompanying constraint was: “This changes CI loss selection; transport recovery and deadlines would stay unchanged.” The operator replied “go ahead”. This approval supersedes the earlier diagnostics-only scope for this implementation; the archived investigation remains an immutable record of the evidence available before approval.

## Fixture contract

The handshake test fixture owns this change. Production QUIC recovery, the 5000-byte payload, the two-minute deadline, RTT settings, TLS configuration matrix and the historical random-loss callback are unchanged. The random callback continues to ignore its requested direction, as tracked separately in issue #79; it is not silently repaired here. PR #144 and the corruption workflow are independent of this branch.

Mandatory `TestHandshakeWithPacketLoss` coverage uses five deterministic schedules in each of the three existing directions, across the five TLS/Retry configurations and three speaking orders (225 cases per QUIC version):

| Schedule | Datagram decisions in the selected direction(s) |
| --- | --- |
| First loss | Drop datagram 1. |
| Initial burst | Drop datagrams 1, 2 and 3. |
| Periodic phase 1 | Drop 1, 4, 7, …, 28. |
| Periodic phase 2 | Drop 2, 5, 8, …, 29. |
| Periodic phase 3 | Drop 3, 6, 9, …, 30. |

Indexes in this table are one based, counted independently per direction. Periodic cases stop introducing losses after datagram 30 and continue to require a successful handshake/transfer within the existing deadline. These are explicit example schedules, not a universal claim that any finite or one-third random loss sequence will complete on time. A short nobody-speaks exchange can finish before a later scheduled drop; the initial-loss cases retain their assertion that a drop occurred.

The three historically named `drop 1/3 of packets` groups are skipped unless `QUIC_GO_TEST_RANDOM_LOSS=1`. Setting that variable restores the original Bernoulli draws, bidirectional loss, ten-consecutive-drop guard, fixture matrix and success assertions. Stress can therefore still fail on a permitted loss sequence; its purpose is exploration, not an unconditional CI success guarantee. Existing diagnostics remain active on failure. No workflow retry, timeout increase, lucky seed or accepted-failure exception is added.

## Captured failure regression

`TestHandshakeCapturedLoss` models the observed application-data mechanism with three scenarios: ACK blackout and missing range (expected read deadline), forward the first missing-range transmission (successful delivery despite absent ACKs), and release one ACK-only datagram (successful recovery). It uses the real transport, a two-endpoint simulator, post-quantum TLS, a long certificate chain, no Retry, the client speaking first, and the unchanged payload, RTT and deadline.

The historical exact datagram-index reconstruction is stored under `docs/audits/issue-44-captured-loss`. It is intentionally not the maintained regression: a race build changed TLS packetization and correctly tripped its expected-fault assertions during development. The maintained model selects the stream frame containing byte 2508, drops up to eight attempts at that range, suppresses application ACKs, and drops repeated non-FIN data while allowing FIN-bearing probes. Eight attempts keep the modeled gap open across the deadline when packetization changes the number of outstanding frames. The original capture had four attempts at the missing range; the regression claims the same mechanism, not the same packet numbers, timing, missing-range boundaries or loss sequence. The ten-consecutive-drop guard applies in every scenario, including after an intervention changes the subsequent traffic. A control changes only its explicit release rule; it does not bypass that guard or change a deadline.

The recorder snapshots scalar frame facts through the existing typed qlog event API. In this fixture, each direction has one connection, PacketSent events precede enqueue to the simulated socket, and GSO is unavailable. Summed packet lengths associate ordered send snapshots with a UDP datagram, including coalesced handshake packets. A mapping mismatch fails the test rather than silently changing which fault was injected. ACK-only release requires a single QUIC packet containing only an ACK. This is a verification aid for that bounded fixture representation, not a general qlog replay engine or encrypted-packet parser.

The timeout assertion requires the actual stream deadline error, the exact contiguous prefix preceding the selected gap, a received suffix and FIN, repeated attempts to send the gap, no gap receipt, no application ACK receipt, and PTO backoff. Both controls require full payload equality/EOF before the deadline and evidence that their intended release occurred. The gap control still requires zero received application ACKs. These observations discharge the regression contract; they do not claim every historical #44 timeout had this cause or promise recovery from every arbitrary blackout.

`TestHandshakeRandomLossOptIn` uses subprocesses to verify the default skip and explicit enable paths while selecting no leaf transfer. Thus its enabled-path check does not accidentally run random stress in mandatory CI.

## Commands

Run the deterministic corpus and regressions:

```sh
go test ./integrationtests/self -run '^TestHandshake(WithPacketLoss|CapturedLoss|RandomLossOptIn)$' -version=1 -count=1
go test ./integrationtests/self -run '^TestHandshake(WithPacketLoss|CapturedLoss|RandomLossOptIn)$' -version=2 -count=1
go test -race ./integrationtests/self -run '^TestHandshake(WithPacketLoss|CapturedLoss|RandomLossOptIn)$' -version=2 -count=1
```

Explicitly request random stress (the regular expression selects all three historical random-loss directions):

```sh
QUIC_GO_TEST_RANDOM_LOSS=1 go test ./integrationtests/self -run '^TestHandshakeWithPacketLoss$/^drop_1$/^3_of_packets_in_direction_' -version=2 -count=1 -v
```

Preserve any failed stress run and its diagnostics before deciding whether another run is informative. Passing duplicates do not diagnose a failure.

## Review and terminating evidence

The supported representation and guarantee are the enumerated example schedules and the frame-observed two-endpoint regression above. Material artifacts are test fixtures, maintained verification aids, documentation and immutable diagnostic receipts. There is no shipped runtime/API change, general replay guarantee, probability claim, direction-policy repair or required random campaign.

The evidence plan is focused v1/v2 tests, one final focused race check, the opt-in gate regression, targeted sensitivity checks of the timeout and ACK control, one final uncached full local suite plus the v2 self suite and documented static checks, followed by the applicable hosted matrix on the exact PR head. Local checks do not stand in for the hosted OS/Go matrix. The repository has inherited push/PR workflows rather than a draft-gated `ci.yml`; use their applicable successful checks as the hosted gate without changing workflows.

The review budget is Standards and Spec reviews, one fully briefed initial RAS review, verification of accepted fixes when needed, and at most one replacement review. Production recovery redesign, recursive testing of evidence tools and random reproduction campaigns are outside this change. Merge only the independently reviewed and locally/hosted validated head of this fixture PR; PR #144 remains owned by its separate workflow.

After this review completed, the operator explicitly approved investigating and repairing a separate MTU fixture failure captured by the initial PR head. The [MTU diagnostic addendum](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/mtu-snapshot-race.md) records that approval, the causal experiment, the narrow fixture repair and its bounded review/evidence plan. The packet-loss contract above is unchanged.
