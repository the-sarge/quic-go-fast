# Wiremux packet-I/O program pointers

The fork contract is the [external packet-I/O plan](2026-09-15-external-packet-io-plan.md), supported by [ADR 0006](0006-explicit-external-packet-io.md).

Program index: [wiremux program index](https://github.com/GridSwarm/wiremux/blob/main/docs/adr/2026-09-14-quic-packet-io-program.md). Track Q contains 13 audited child slices. Live state and issue mapping belong to the [program tracker](https://github.com/GridSwarm/wiremux/issues/1540). Q01–Q05, R01-A/B, R01-L/W, R03, P01-A/B, E02 and L01 are complete. The cross-repository program closed through Z01; the fork release is [v0.62.1-fast.3](https://github.com/the-sarge/quic-go-fast/releases/tag/v0.62.1-fast.3), and wiremux retains its selected-peer wrapper. Final assembled performance comparisons were unavailable, so ordinary defaults and performance non-claims remain in force. Follow-up capability qualification is tracked separately in [#353](https://github.com/the-sarge/quic-go-fast/issues/353) and [#354](https://github.com/the-sarge/quic-go-fast/issues/354).

Per slice: `$implement-architecture-slice`, repository review/validation gates, merge, append-dev-journal, then reconcile issue and OmniFocus pointers. Only program Z01 may close the parent.
