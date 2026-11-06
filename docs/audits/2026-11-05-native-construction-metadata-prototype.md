# Construction metadata prototype for N3 #1202

**Verdict:** implementation and ownership evidence support retaining the prototype for further design work, but its measurement pair is **ineligible for a comparative performance conclusion**. The baseline accumulated 21.612077677 seconds of foreign CPU on the assigned test CPUs and their SMT siblings; the candidate accumulated 0.336358741 seconds. The baseline was classified as contaminated. Neither observation was rerun.

The latest clean result remains source `d68633ed668653401520985914cda7ee831b99e9`: **76.2% of baseline goodput and 127.0% of baseline CPU per useful byte**, missing the unchanged 95% goodput floor and 110% CPU ceiling. [N3 #1202](https://github.com/the-sarge/quic-go-fast/issues/1202) remains stopped and [draft PR #1230](https://github.com/the-sarge/quic-go-fast/pull/1230) remains held. This prototype establishes neither a new efficiency result nor production qualification.

## Frozen source and ownership

The measured source is `c0138fc27529dc73a439c4a04a0b9c2cf27feb3f`, based on `d68633ed668653401520985914cda7ee831b99e9`. Source archive SHA-256: `5925d067aa2bdf09378d4e11c8cb9e4fc4efb333790c496346a1daf75c14d1e4`. Recorded binary SHA-256: `d2365d47ba885efea28fa33597a036371bc17328e81ac789fd72d05ba028b0f4`.

Ordinary local construction now obtains the existing pooled recovery packet before asking the framer for stream metadata. `PacketPreparation` lends that attempt an empty stream slice and registration adopts the final slice, including growth. Preparation reserves no packet number, history capacity, flight or native storage. Sealing and packet-number commitment keep their existing order; ordinary registration validation still occurs after each seal. Refusal leaves preparation owned by the caller; packing or registration failure discards its metadata without invoking callbacks or refunding packet numbers. Success consumes the handle before registration callbacks.

A prepared packet can retain one cleared stream-frame slot after ACK retirement. ACK callbacks finish before the canonical registration loses its packet reference and the packet returns to the pool. Late native completion therefore cannot reach a reused slot. Loss, Retry, key disposal, migration and close retain their previous non-pooling behavior for registered packets. Multi-frame backing is not cached. Other emission classes continue through their existing paths.

`LocalCompletionToken` now contains only the canonical registration pointer, removing its duplicate epoch. Canonical slots are issued once and never manually reused; the canonical record still carries the epoch used to protect replacement history. Owner and consumed-outcome checks remain. The retained local layout probe reports an 8-byte token instead of 16 bytes. This is structural evidence, not a throughput claim.

## Behavioral evidence

The whole root package passed in 11.965 seconds and the full recovery race suite in 71.369 seconds. Corrected focused ownership races passed in 1.548 seconds for recovery and 1.407 seconds for the root package; pointer-token races also passed. Vet, tidy-diff and Linux semantic preconditions passed. Two initial local compile failures, caused by a test call missing the new internal argument, remain preserved beside the corrected results.

Tests cover alias clearing, nil and repeated discard, refusal ownership, adoption after validation, slice growth, ACK before all late completion outcomes, and reentrant discard during registration callbacks. Independent authorship source reviews found no blocking ownership or lifetime defect. One nonblocking comment still describes pooling as ACK-only, overlooking discard of never-registered preparations.

Compiler evidence shows the preparation handle inlined without a moved-to-heap diagnostic; the prepared parameter's content escapes because it transfers the existing packet. This does not guarantee pool hits, allocation-free growth or identical compiler behavior on every target.

## What the profiles establish

Both roles completed successfully using one Linux amd64 Go 1.27.3 binary, seed 1038, 10-second warmup and a 60-second window. Server CPUs were 8–11 and client CPUs 12–15; foreign CPU monitoring included their SMT siblings. Successful roles do not repair the contaminated comparator. Raw ratios remain archived but are not promoted to a new comparative result.

The candidate profile attributes approximately **0.018669354 ns/useful byte** to construction metadata preparation outside registration. Packet acquisition moved before the packer; a smaller registration row is therefore not wholly eliminated work. CPU attribution scales whole-role sample shares by measured-window CPU/byte. Memory profiles likewise cover the entire role at different work volumes, so their totals are not exact window allocation rates. These profiles identify mechanisms to investigate, not a causal performance benefit or an uninstrumented result.

## Evidence and accounting

Evidence is retained under `/Users/josh/diagnostics/quic-go-fast/issue-1202-pipeline-2026-11-05/construction-metadata-v1/`, with the source archive and local-validation directory adjacent. Thirty-six run artifacts plus the now-local archive were independently hash-checked; its commit marker and eight fixture hashes match. The analysis predates the archive's arrival and still lists it as unavailable. The remote-only binary's recorded identity is consistent but was not independently rehashed locally.

The build and two Linux semantic invocations consumed 28.811279142 seconds; the two observations consumed 154.104556230. This adds **five counted invocations / 182.915835372 measured host-seconds**, plus **4.160048995 seconds** of separately recorded preparation and quiet checks. Cumulative history becomes **193 invocations / at least 3593.787869676 measured host-seconds**, retaining previous failures, contamination and separately recorded overhead.

Recommendation: retain the source and lifetime improvements as candidates for the broader redesign, preserve this contaminated observation without a new performance claim, and use the latest clean checkpoint as the efficiency reference until another authorized comparison supplies eligible evidence.
