# quic-go-fast first milestone — 2026-09-06

**Program identity:** QGF-2026-09. **Status:** D and H complete. **Normative scope:** This index owns track identity, dependency edges and binding policy; each plan owns its slice contracts. **Audit history:** [Handoff audit](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-06-handoff.md).

## Outcomes and boundaries

Improve native datagram receive efficiency for a 1071-byte bulk workload and recover from explicit oversized handshake sends on paths that support 1200-byte UDP payloads. Preserve existing API contracts and wire interoperability. Adopt the fork through application-owned pinned replacements. Base releases on validated stable upstream releases, reviewing security fixes promptly; upstream acceptance does not gate fork releases. See ADRs [0001](0001-upstream-compatibility.md), [0002](0002-adopt-through-module-replacement.md), and [0003](0003-follow-stable-upstream-releases.md).

The code baseline is upstream v0.62.0 (`793f74d8e03368c5aded128af6f48d21dbb47f73`). `main` starts from that release; the imported `master` remains available as historical upstream state. Later merged milestone slices may be present when a child starts; review only their relevant governing diff. Each benchmark compares a candidate with its exact parent without that candidate, and labels any comparison with pure v0.62.0 separately. No automatic upstream-development tracking or reset of another worktree is authorized.

## Tracks and frontier

| Track | Plan | Parent issue | Slices | Blocking edges | Current state |
| --- | --- | --- | --- | --- | --- |
| D — Datagram receive efficiency | [Plan](2026-09-06-datagram-plan.md) | [#2](https://github.com/the-sarge/quic-go-fast/issues/2) | D1, D2 | None | Complete: D1 runtime improvement; D2 bounded no-change |
| H — Handshake MTU recovery | [Plan](2026-09-06-handshake-mtu-plan.md) | [#3](https://github.com/the-sarge/quic-go-fast/issues/3) | H1 | None | Complete in [#20](https://github.com/the-sarge/quic-go-fast/pull/20) |

D1 and D2 are independent but edit the same queue module: their worktrees may run in parallel, but serialize integration and revalidate the later merge against the earlier patch. H1 changes the connection/send path and can proceed alongside either. Shared test fixtures or same-file merge conflicts are integration obligations, not invented blocking edges. The implementation frontier is empty: H1 (#6) is complete in [#20](https://github.com/the-sarge/quic-go-fast/pull/20); there are no pending successor slices. D1 (#4) is complete with a runtime improvement; D2 (#5) is complete with a bounded no-change disposition. Parent issues are never dispatched as implementation tasks.

## Outcomes requiring no implementation

Jumbo-packet support is deferred. No new application API, consumer datagram adoption, application multipath scheduler, custom congestion algorithm, or performance-sweep replacement is included. External integration/qualification remains externally owned. No particular packet-rate improvement or physical-link capacity is promised. D1/D2 may close with a bounded no-change finding if their evidence rejects the optimization; track the actual disposition.

## Rules binding every track

The shared REVIEW-LOOP.md and CONTRACT-CLOSURE.md baselines supplied by `$implement-architecture-slice` govern. No repository copies or stronger overlays are introduced. Use their independent finding dispositions, one initial review plus at most one replacement, non-recursive evidence boundaries, representation gate, and precise-invariant/owner approach stops. The applicable local baselines reside in the installed skill's `../_shared/` directory and must be read at dispatch.

Each slice uses its own worktree and feature branch, a draft PR, its finite local validation evidence on the exact final pushed head, applicable same-head hosted checks, squash merge guarded by the live head, then `$append-dev-journal`, and finally task completion. Inspect actual repository checks: this fork inherits upstream workflow names rather than the portfolio `ci.yml`/`ci-*` naming. Do not invent skipped-job success, bypass a required check, or expand this program into CI standardization. Failure requires diagnosis before a rerun; unavailable operational evidence is unqualified rather than a pass.

For this documentation package, the evidence domain is the authored Markdown and the three explicit slice contracts. One independent slice audit, file/link/format inspection, and whitespace validation terminate the docs gate. No runtime test, performance capture, CI workflow modification, or implementation agent belongs to this handoff. Hosted checks required by actual protection rules must still pass on the merge head.

Program trackers and task notes hold current state and pointers, not copies of contracts. Child issues carry the exact merged plan commit; task notes do not. Current readiness is distinct from later code certification. Context is cleared between dispatched child issues.
