# Wiremux packet-I/O program pointers

The fork contract is the [external packet-I/O plan](2026-09-15-external-packet-io-plan.md), supported by [ADR 0006](0006-explicit-external-packet-io.md).

Program index: [wiremux program index](https://github.com/GridSwarm/wiremux/blob/main/docs/adr/2026-09-14-quic-packet-io-program.md). Track Q contains 13 audited child slices. Live state and issue mapping belong to the program tracker; pending until this handoff publishes both plans. Q01, R01-A and P01-A form the initial fork frontier. E01 can proceed independently in wiremux. No runtime implementation is claimed or dispatched.

Per slice: `$implement-architecture-slice`, repository review/validation gates, merge, append-dev-journal, then reconcile issue and OmniFocus pointers. Only program Z01 may close the parent.
