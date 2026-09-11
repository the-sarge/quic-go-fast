# Large accepted stream-write storage

Issue: https://github.com/the-sarge/quic-go-fast/issues/75

## Acceptance criteria (verbatim)

- [ ] Remove quadratic tail copying through tryWriteAll and popNewStreamFrame while preserving atomic admission and caller-buffer ownership.
- [ ] Use bounded allocation/copy-scaling checks across increasing accepted payload sizes and cover cancellation and retransmission storage lifetime.
- [ ] Keep flow-control, pacing, recovery and public API semantics unchanged; avoid a new buffer-ownership API.
- [ ] Do not launch an archived performance campaign or claim an application throughput gain without evidence.


## Representation and scope

The supported domain is bytes accepted atomically by `SendStream.TryWriteAll`, including repeated admissions and appends after partial packetization, and their existing packetization, ACK/loss, cancellation and reliable-prefix paths. `SendStream.nextFrame` owns pending bytes; emitted STREAM frames own packet-sized recovery storage. No caller storage is retained. The algorithmic guarantee is linear total copying in admitted bytes with bounded packet-size tail work, supported by finite scaling checks at 32, 128 and 512 KiB for single admissions and 2 KiB admissions. This is not an application throughput claim.

Production changes are confined to growth and segmentation in `send_stream.go`. Tests are verification aids. Public API/module identity, atomic flow-control reservation, pacing, packet emission, recovery accounting and reset final-size semantics remain unchanged. Existing issues #18, #44 and #46 and archived experiments remain intact. No new buffer-ownership API, performance campaign, timing threshold, mutation campaign or platform cross-product expansion is included.

## Terminating evidence and review

Use the existing `TryWriteAll` / packetization / recovery test seams named by the issue. Evidence comprises a red/green recovery-storage regression, bounded allocation and no-tail-copy checks across the sizes above, rejected admission and caller reuse, appends after partial drain, retransmission splitting, and cancellation with zero, short and large reliable prefixes. Run affected stream tests during implementation, one focused race run, one uncached full local suite, `go vet ./...`, `go mod tidy -diff` and formatting checks on the final candidate. No benchmark throughput assertion is needed.

Review allowance: independent Standards and Spec reviews, one fully briefed initial RAS review with two configured reviewers (`codex-astra`, `codex-sol`) and a 900-second per-agent limit, verification of accepted fixes if necessary, and at most one replacement review. Independently disposition findings under the shared review loop; no automated fixer. Stop for a required architectural or contract expansion, rather than silently widening this PR.

This repository has upstream-style push/pull_request workflows, including on drafts, and no `task preflight`, `ci.yml` or `ci-*` jobs. Use the existing applicable hosted checks on the exact live head, all successful and unskipped, as the CI gate. Do not alter CI or rerun successful draft checks to create portfolio gate names. Mark ready after review/local certification, verify the head/base, then squash merge using `--match-head-commit`. Journal only after the product merge, then complete the OmniFocus task. Keep review history and exact-head receipts outside this normative contract.
