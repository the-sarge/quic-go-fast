# Wiremux packet-I/O program pointers

The fork contract is the [external packet-I/O plan](2026-09-15-external-packet-io-plan.md), supported by [ADR 0006](0006-explicit-external-packet-io.md).

Program index: [wiremux program index](https://github.com/GridSwarm/wiremux/blob/main/docs/adr/2026-09-14-quic-packet-io-program.md). Track Q contains 13 audited child slices. Live state and issue mapping belong to the [program tracker](https://github.com/GridSwarm/wiremux/issues/1540). Q01, R01-A/B and P01-A/B are complete. Q02–Q05 are the current fork frontier after retiring duplicate E01-L/W/D baseline campaigns. Focused native correctness remains required; E02 owns the final assembled comparison. Wiremux retains its selected-peer wrapper. No implementation is dispatched by this revision.

Per slice: `$implement-architecture-slice`, repository review/validation gates, merge, append-dev-journal, then reconcile issue and OmniFocus pointers. Only program Z01 may close the parent.
