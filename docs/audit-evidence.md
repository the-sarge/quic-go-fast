# Archived audit evidence

Historical audit receipts, captures, profiles and experiment helpers remain at `docs/audits/` in Git. Go's nested-module boundary excludes that entire subtree, including its Markdown receipts, from the parent dependency archive. The marker is packaging metadata only: it has no dependencies or exported library role. See the [module payload contract](adr/2026-09-08-module-payload-plan.md).

This index ships with the parent module. The links below identify the evidence snapshot at commit `3db9121a2c91bce8acb7b6f871adfcaf51bc560b` and preserve the archived files' original bytes and paths. Browse the [complete audit tree](https://github.com/the-sarge/quic-go-fast/tree/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits) for raw files alongside each receipt. These are historical evidence, not current execution instructions or maintained validation tools.

Historical [development journal](DEV-JOURNAL.md) paragraphs are unchanged. Their checkout-relative `audits/` links cannot resolve inside the Go module cache: find the same path below or in the pinned tree. Follow links within a receipt on GitHub, or check out that commit to use its original relative paths locally. Later evidence can be indexed at its own immutable commit without rewriting this snapshot.

| Archived document (relative to `docs/audits/`) | Immutable reference |
| --- | --- |
| `2026-09-06-d1-overflow.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-06-d1-overflow.md) |
| `2026-09-06-d2-burst-protocol.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-06-d2-burst-protocol.md) |
| `2026-09-06-d2-burst-results.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-06-d2-burst-results.md) |
| `2026-09-06-d2-linux-protocol.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-06-d2-linux-protocol.md) |
| `2026-09-06-d2-linux-results.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-06-d2-linux-results.md) |
| `2026-09-06-d2-paced-protocol.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-06-d2-paced-protocol.md) |
| `2026-09-06-d2-paced-results.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-06-d2-paced-results.md) |
| `2026-09-06-d2-receive-storage.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-06-d2-receive-storage.md) |
| `2026-09-06-datagram-native-comparison.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-06-datagram-native-comparison.md) |
| `2026-09-06-datagram-ownership.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-06-datagram-ownership.md) |
| `2026-09-06-datagram-probe-timing.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-06-datagram-probe-timing.md) |
| `2026-09-06-datagram-tail-followup.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-06-datagram-tail-followup.md) |
| `2026-09-06-handoff.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-06-handoff.md) |
| `2026-09-07-d2-evidence-closeout.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-07-d2-evidence-closeout.md) |
| `2026-09-07-datagram-adoption.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-07-datagram-adoption.md) |
| `2026-09-07-datagram-core-budget.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-07-datagram-core-budget.md) |
| `2026-09-07-e3-certification-exception.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-07-e3-certification-exception.md) |
| `2026-09-07-e3-certification-handoff.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-07-e3-certification-handoff.md) |
| `2026-09-07-emission-adoption-decision.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-07-emission-adoption-decision.md) |
| `2026-09-07-emission-deployment-budget.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-07-emission-deployment-budget.md) |
| `2026-09-07-emission-four-core-resumption.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-07-emission-four-core-resumption.md) |
| `2026-09-07-packet-emission-handoff.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-07-packet-emission-handoff.md) |
| `2026-09-08-architecture-handoff/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-08-architecture-handoff/README.md) |
| `2026-09-08-architecture-handoff/grilled-assessment.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-08-architecture-handoff/grilled-assessment.md) |
| `2026-09-08-e6-contract-reslice.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-08-e6-contract-reslice.md) |
| `2026-09-08-emission-construction/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-08-emission-construction/README.md) |
| `2026-09-08-emission-construction/investigation.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-08-emission-construction/investigation.md) |
| `2026-09-09-http3-fixture-completion.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-09-http3-fixture-completion.md) |
| `2026-09-10-i3-empty-input.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-10-i3-empty-input.md) |
| `active-sendconn-publication.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/active-sendconn-publication.md) |
| `d2-burst/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/d2-burst/README.md) |
| `d2-paced/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/d2-paced/README.md) |
| `datagram-core-budget/results-table.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/datagram-core-budget/results-table.md) |
| `datagram-native-comparison/results-table.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/datagram-native-comparison/results-table.md) |
| `datagram-ownership/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/datagram-ownership/README.md) |
| `datagram-tail-followup/results-table.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/datagram-tail-followup/results-table.md) |
| `e1-emission-four-core/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/e1-emission-four-core/README.md) |
| `e1-emission-four-core/review.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/e1-emission-four-core/review.md) |
| `e1-emission/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/e1-emission/README.md) |
| `e1-emission/review.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/e1-emission/review.md) |
| `e2-buffer-lifetimes/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/e2-buffer-lifetimes/README.md) |
| `e2-buffer-lifetimes/review.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/e2-buffer-lifetimes/review.md) |
| `e3-packet-emission/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/e3-packet-emission/README.md) |
| `e4-handshake-emission/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/e4-handshake-emission/README.md) |
| `e5-probe-emission/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/e5-probe-emission/README.md) |
| `e6-close-emission/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/e6-close-emission/README.md) |
| `e6a-handshake-consumers/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/e6a-handshake-consumers/README.md) |
| `e6b-scheduling-consumers/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/e6b-scheduling-consumers/README.md) |
| `e6c-path-consumers/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/e6c-path-consumers/README.md) |
| `issue-44-captured-loss/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/issue-44-captured-loss/README.md) |
| `issue-46-captured-corruption/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/issue-46-captured-corruption/README.md) |
| `issue-46-captured-corruption/development/LEDGER.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/issue-46-captured-corruption/development/LEDGER.md) |
| `issue-46-recovery/LEDGER.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/issue-46-recovery/LEDGER.md) |
| `issue-46-recovery/MERGE-CONTRACT.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/issue-46-recovery/MERGE-CONTRACT.md) |
| `issue-46-recovery/PLAN.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/issue-46-recovery/PLAN.md) |
| `issue-46-recovery/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/issue-46-recovery/README.md) |
| `issue-46-recovery/REVIEW.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/issue-46-recovery/REVIEW.md) |
| `issue-46-strengthening/PUBLICATION.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/issue-46-strengthening/PUBLICATION.md) |
| `issue-46-strengthening/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/issue-46-strengthening/README.md) |
| `issue-46/LEDGER.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/issue-46/LEDGER.md) |
| `issue-46/PLAN.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/issue-46/PLAN.md) |
| `issue-46/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/issue-46/README.md) |
| `issue-46/REVIEW.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/issue-46/REVIEW.md) |
| `mtu-snapshot-race.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/mtu-snapshot-race.md) |
| `pacing-ci-103/README.md` | [View](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/pacing-ci-103/README.md) |
