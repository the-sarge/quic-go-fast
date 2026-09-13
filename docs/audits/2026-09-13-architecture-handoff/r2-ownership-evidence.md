# R2 incoming STREAM ownership evidence

Contract: [R2 — Consume incoming STREAM frame ownership through dispatch](../../adr/2026-09-13-receive-stream-lifetime-plan.md#r2--consume-incoming-stream-frame-ownership-through-dispatch), issue #268. Starting code: `f90f9eeb17068cdebb82006e6c8c0d5d8096f70e`; accepted plan pointer: `5072c121643cd8c0fd8d8913b2cbcf7ecb5e992f`. The governing diff changed R1 completion only.

## Bounded release observation

A temporary observer immediately before `pool.Put(f)` counted concrete pooled STREAM returns. It ran only in serial focused tests, checked frame identity at map/receive seams, and was removed with its assertions before commit. It did not infer release from pool reuse or GC timing. During the skipped-dispatch test the observer also changed the released frame's metadata and data length, demonstrating that qlog had already captured independent values. No production observation hook or global pool tracker remains. The following are historical red/green observations, not a maintained harness or a completeness proof.

| Case | Before its owner fix | After its owner fix | Retained evidence |
|---|---|---|---|
| Parser allocation larger than pool capacity | Expected 1 release; observed 0 | 1 release | `TestParseStreamFrameRejectsLongFrames` |
| Parser maximum-offset overflow with 128-byte pooled data | Expected 1 release; observed 0 | 1 release | `TestParseStreamFrameRejectsOverflow/128`; original 6-byte nonpooled case retained |
| STREAM after earlier STOP_SENDING handling error | Expected 1 release; observed 0 | 1 release; qlog values unchanged after release | `TestConnectionSkippedStreamFrameMetadata` |
| Invalid receive-stream lookup / deleted stream | Both expected 1 release; observed 0 | 1 release each | `TestStreamsMapRejectedStreamFrame` |
| Shutdown / local cancel / flow-control rejection / final-size rejection | Each expected 1 release; observed 0 | 1 release each | `TestReceiveStreamRejectedStreamFrame` |
| First fatal sorter gap-limit rejection | Incoming callback not called | Callback called once | `TestFrameSorterTooManyGaps` uses the existing double-call guard |
| Trim-to-copy success | Existing behavior preserved | Incoming callback called; Pop transfers no callback for the copy | Strengthened `TestFrameSorterSimpleCases` |

These eleven semantic cases stay within the twelve-case ceiling and reuse existing parser and sorter cases. Permanent map/receive/parser tests check rejection behavior; concrete release counts at those seams were temporary evidence as the contract permits. Existing sorter gap/overlap cases retain callback assertions for unique, duplicate and replaced entries. The fatal sorter is not reused, and no trim-copy-then-first-gap-error scenario is claimed. Crypto continues to pass a nil callback.

## Validation

Focused instrumented tests passed together before observer removal. Post-removal affected-package, focused race, full-suite and exact-head certification results are recorded in the product PR and execution receipt. Runtime ownership remains with successive concrete owners; no public API, wire representation, UDP storage, terminal retirement, or flow-control algorithm changes are included.
