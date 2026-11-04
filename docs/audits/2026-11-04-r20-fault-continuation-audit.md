# R20 endpoint-failure continuation slice audit

## Trigger

During [R13 #1033](https://github.com/the-sarge/quic-go-fast/issues/1033), the R14 re-audit said R13 owned path-level continuation after an endpoint fault, while R13's contract never listed it and its author confirmed the clause wasn't a checked assignment. The base plan requires it: the native error table says healthy paths on other endpoints stay, and public contract item 8 says network I/O errors are attributed to their affected paths and sockets. On 2026-11-04 the owner chose a separate slice before public enablement, not required before R16. The [plan's R20 section](../adr/2026-10-24-multipath-reno-plan.md#r20) owns the contract. This receipt records the source facts and routing only.

## Source facts at `1f4cc10ae36ceac176710233b554155fc24c8c4f`

| Fact | Evidence | Consequence |
| --- | --- | --- |
| A closed socket reaches the connection twice | `consume` sets `errEndpointFault` from `s.endpoint.fault` (`native_ordinary.go:616`), and the read loop calls `t.close(err)`, which runs `destroy` on every handler (`transport.go:791`, `:677`) | Changing the fault policy alone wouldn't keep the connection alive |
| Only a read failure closes with a non-nil cause | `Transport.Close` calls `t.close(nil)` (`transport.go:618`) | Explicit close stays distinguishable |
| The fault channel stays ready once closed | `case <-c.emission.localFault()` (`connection.go:758`) | Continuing without quieting it would spin |
| A fault wakes every participant | `fail` signals each `completed` (`native_endpoint.go:988`) | The quiet channel loses no wake |
| The writer exits without waiting for participants | `s.sealed && s.current == nil && len(s.queue) == 0` (`native_endpoint.go:724`) | A spared connection can't block `waitNativeIOStopped` |
| `routeUsable(nil)` assumes the primary is healthy | `native_ordinary.go:316` | Path zero would stay eligible on a failed endpoint |
| A primary's fatal result fails the connection | `participant == s.participant` (`native_ordinary.go:673`) | A late result could end it even with the fault handled |
| Detachment refuses any closed transport | `detachRoutes` tests `transportClosed` (`transport.go`) | A spared connection could never publish detachment |
| Sealing discards a participant's later results | `run` revokes active participants once sealed, and `publish` clears the record of an inactive one (`native_endpoint.go`) | A spared connection needs its own settlement, as uncertain, after the participant retires |
| An explicit close after a read failure is a no-op | `close` returns at `transportClosed` (`transport.go:660`) | Explicit closure has to be its own fact |
| Reusing an attachment skips the closed-target check | `attach` returns early on an existing entry (`multipath_migration.go:73`) | Operations need a target check before commitment |
| R17 and R18 pin the complete Reno candidate, and R19 stops if changed code invalidates a candidate | The plan's candidate ledger, and R19's stop | R20 has to precede those two, not only R19 |

## Graph decisions

R20 has no open blockers, since R13, R14 and R15 are complete. It blocks R17 and R18 for the candidate reason above, and R19 only transitively. It doesn't touch R16's API surface, so the two are parallel-safe, with whichever lands second reconciling against current main. Rejected split and merge alternatives are in the contract.

## Review record

RAS consideration `20261104T163006-80bb852e7e8f90d4de28b890` failed in synthesis because the model was at capacity, so it isn't review evidence. Its rerun `20261104T170535-714433170851deac0620db26` on `b56a0d2a3b875bac14f992e9bd09146708dffce3` is the initial review. C-007 with C-009, C-002 with C-005 and C-006, C-004 (narrowed), C-010 and C-003 were fix-now and are corrected in the contract. C-001 was rejected, because a non-returning native call is outside the progress guarantee and its fence must hold. C-008 was rejected, because a validating path hasn't proved healthy; its dissent is recorded and the contract states the outcome.
