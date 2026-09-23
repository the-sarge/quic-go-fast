# Windows managed ECN implementation plan

**Date:** 2026-09-23
**Status:** W1 and W2 complete; W3 implementation is the frontier
**Track:** W, 5 of 5 in `QGF-ECN-20260922`
**Depends on:** W1 and the merged L1 managed adapter; no incomplete cross-track blockers
**Related:** [Program index](2026-09-22-managed-ecn-program.md), [ADR 0001](0001-upstream-compatibility.md), [ADR 0006](0006-explicit-external-packet-io.md), [ADR 0007](0007-managed-ecn-qualification.md), [Windows datapath plan](2026-09-11-windows-datapath-plan.md)
**Normative scope:** Current outcome, boundaries, invariants, acceptance evidence, blockers and stop conditions
**Audit history:** [Original program audit](../audits/2026-09-22-managed-ecn-handoff/README.md), [W1 native receipt](../audits/2026-09-23-windows-ecn-w1/README.md), [Windows successor slice audit](../audits/2026-09-23-windows-ecn-handoff/README.md)

## Goal

Enable managed ECN through the existing endpoint and exact lease on native Windows 11 and Windows Server 2025, for IPv4-only, IPv6-only and dual-stack sockets including mapped IPv4 traffic. First resolve the remaining native representation questions, then deliver receive and send together in one implementation PR. Keep native socket authority private and ordinary datagrams, filtering, batch progress and lifetime behavior intact.

## Current shape (verified at 260a51f6c76129e6972b223e42d6c52c464fa442)

- `sys_conn_windows.go:39-102,147-248` owns message I/O, packet info, USO and URO. It has no ECN capability, panics for marked sends, uses a small ordinary receive buffer, and normalizes successful send counts only for USO.
- `sys_conn_windows.go:290-402` owns the architecture-sized `WSACMSGHDR` layout and bounded packet-info/coalescing parsing. `coalesced_delivery.go` already preserves the original packet metadata on segment views.
- `managed_packet_receive_windows.go:5-40` has no managed raw factory and retains a native reader only when URO activates. `managed_packet_endpoint.go:69-150,178-199,282-323` owns setup, exact lease identity, read metadata and the operation-scoped checked singleton route.
- `managed_packet_ecn_unix.go:17-187` is platform-neutral adapter logic behind a Unix build constraint. Its generation/buffer/range/address correlation and checked send route are the existing enforcement seam; Windows must reuse it, not copy its state machine. Unix family inspection in `managed_packet_receive_ecn.go:21-71` imports x/sys/unix and must remain platform-specific.
- `external_packet_io_no_oob.go:1-11` currently makes Windows external ECN append a no-op. `external_packet_io.go:192-215` already normalizes a successful zero-byte Windows message send in ordinary batches while retaining short-write/OOB and definite-prefix checks. The missing normalization is the endpoint-owned native send path used by both direct marked sends and checked singleton forwarding, not this existing batch writer.
- W1 establishes the native integer representation on Server 2025 build 26100.32230/amd64 with Go 1.27.0 and x/sys 0.48.0 for separate `udp4` and `udp6` sockets. It does not qualify Windows 11, Go 1.26, dual-stack, wrappers or ECN/URO interaction.

## Decisions and boundaries

Use `IP_RECVECN` and `IPV6_RECVECN` directly. The [Microsoft Winsock ECN contract](https://learn.microsoft.com/en-us/windows/win32/winsock/winsock-ecn) requires separate family options on a dual-stack wildcard socket; deprecated convenience helpers are not the representation owner. Receive `IP_ECN` / `IPV6_ECN` contains a native 32-bit integer codepoint. The relevant option/message constants are 50 at IPPROTO_IP or IPPROTO_IPV6. Derive header alignment from x/sys Windows types, never hard-code the W1 amd64 header size as a portable ABI. Windows can receive CE but rejects application-generated CE with WSAEINVAL; preserve that native error rather than inventing an outgoing CE capability.

Windows 11 and Server 2025 are two required native qualification targets. Select one available maintained build of each, record exact edition/build/architecture, and exercise the repository's supported Go 1.26 and 1.27 lines. This is a finite desktop/server validation set, not an assertion that every historical Windows 11 release works. W2 records exact revisions. Server 2022, Windows 10, older client releases, ARM64 native qualification and an OS-version support matrix are non-goals. Existing cross-compilation remains required. Runtime availability uses the admitted-family socket-option result and opt-out policy, not an edition/build allowlist. Unqualified releases may work through these APIs but gain no native support claim from these receipts; option denial retains usable ordinary I/O.

The endpoint remains the sole owner of native setup, qualification, retained receive storage and Close. Configure under its lock after ordinary I/O joins. Inspect the concrete socket's native family, binding and IPV6_V6ONLY; qualify each actually admitted family, including mapped IPv4. Unknown family inspection fails ECN closed. A failed unused family must not disable a single-family endpoint; a failed admitted family disables ECN for the whole dual-stack endpoint. `QUIC_GO_DISABLE_ECN` disables managed ECN independently of URO. Rebinding a lease refreshes the availability policy without replacing a retained URO decoder or creating a second socket/lifetime owner.

Only endpoint-owned Windows message I/O gains managed ECN. Reuse the shared adapter with suitable build constraints and a Windows factory, preserving its exact forwarding contract. Unknown wrappers and callbacks that normalize results remain unqualified. Direct, checked singleton and registered ordinary batch sends use native Windows ECN OOB; outgoing Not-ECT, ECT(0) and ECT(1) must reach an independent peer. Managed GSO remains false. Unregistered Windows ECN capability remains false and its existing packet-info/USO behavior is preserved.

Normalize a successful zero-byte payload result to the submitted payload length only in the endpoint-owned Windows native send mode, including the checked singleton's ECNUnsupported argument carrying already-encoded OOB. Leave nonzero short results and native errors intact. Existing ordinary batch normalization remains its owner for that route. Do not normalize arbitrary wrapper callback results or treat an error plus zero bytes as success. W1 traced the zero result to a Go overlapped completion path; do not generalize it to every WSASendMsg completion.

Managed non-URO ECN reads use full accepted UDP-datagram storage, one native read per datagram, and no read-ahead across lease return. Missing ECN delivers the payload with ECNUnsupported. Malformed/truncated managed ancillary data or payload is discarded before metadata publication; a subsequent valid datagram must still arrive. A retained URO decoder continues normalizing across leases and ECN opt-out; an ECN-only reader is not retained solely for disabled ECN. GRO/receive_enabled reflect actual URO activation, not reader presence. Packet info is not imported into the caller's outer raw connection by the ECN adapter.

URO and ECN share one Windows decoder. [Microsoft's URO rules](https://learn.microsoft.com/en-us/windows-hardware/drivers/network/udp-rsc-offload) require equal ECN across coalesced segments; propagate the parsed codepoint through the existing segment holder. W2 checks native coexistence and records whether actual coalescing occurred. W3 uses a focused decoder/segment fixture for ECN-bearing coalesced input even if the native NIC does not engage URO. Hardware engagement, throughput and timing guarantees are not acceptance conditions.

**Do not do this:** Do not infer receive/send support from option success, server evidence alone, packet info, USO/URO or cross-compilation. Do not copy the managed adapter into a Windows state machine, expose raw handles, enable ECN for unregistered connections, use an OS-version sniff, add a generic Windows compatibility framework, suppress native CE errors, or put runtime packet probes in connection setup.

**Non-goals:** Other platform implementation; Windows networking/offload redesign; public APIs or dependencies; QUIC recovery changes; raw socket access; performance work; exhaustive kernel, NIC, Windows release or toolchain patch matrices; maintained VM provisioning or packet-probe infrastructure.

## Slice graph

| Slice | Status/disposition | Delivers | Blocked by | Removes temporary seam |
| --- | --- | --- | --- | --- |
| W1 | Complete; retain #507 / #534 | Separate-family native feasibility receipt | None | Probe retired in W1 |
| W2 | New; frontier; child pending | Desktop/server and dual-stack native qualification receipt | W1 complete | Disposable probes retired in W2 |
| W3 | New; blocked; child pending | Complete managed Windows ECN receive/send qualification | W2; L1 complete | None; no runtime intermediate form |

## Implementation slices

### Slice W1 — Decide Windows native ECN feasibility

**Stable identity:** `QGF-ECN-20260922/W1`; completed [#507](https://github.com/the-sarge/quic-go-fast/issues/507) through [#534](https://github.com/the-sarge/quic-go-fast/pull/534). Retain the [frozen native receipt](../audits/2026-09-23-windows-ecn-w1/README.md). No runtime capability or dormant encoder/parser was introduced. Its historical contract and evidence remain available at its merged plan commit; do not repeat or redispatch it.

### Slice W2 — Qualify Windows 11 and dual-stack native ECN

**Stable identity:** `QGF-ECN-20260922/W2`. Complete: [#537](https://github.com/the-sarge/quic-go-fast/issues/537); [native qualification receipt](../audits/2026-09-23-windows-ecn-w2/README.md). The accepted representation and all four required traffic paths are confirmed on the two recorded OS builds and both Go lines; W3 is eligible. Runtime ECN remains unchanged.

**What it delivers:** A reproducible native disposition for the remaining platform/family/runtime questions, precise ABI observations and send-result interpretation, permitting W3 to start only when its accepted representation is confirmed. Windows runtime ECN remains false.

**Existing-work disposition:** Retain W1 and the merged Windows datapath. No open implementation PR exists at audit baseline. This is new bounded evidence, not a revision of frozen W1 files.

**Blocked by:** W1, already complete. No Linux, OpenBSD or other incomplete work. W2 is independently green because it changes only current status and adds its own frozen receipt.

**Single owner / authority completeness:** No new runtime fact. The native OS/Go boundary owns observations; the new receipt owns their example-level qualification claim. Record construction, inspection, values, independently observed outgoing marks and cleanup. No persisted runtime state or restart migration is introduced.

**Transitional seams:** None in runtime. Disposable probe source, binaries and task-owned VM overlays are retired after capture; retain only concise commands, environment identity, observed values and cleanup evidence. Existing immutable images may be used with disposable overlays. Do not turn provisioning or the peer into a maintained deliverable.

**Blast radius and artifacts:** Documentation/status changes are process metadata. Native probes and independent Linux peer are verification aids, limited to task-owned sockets/resources. No production Go, dependency, public interface, kernel configuration outside disposable guests, shared guest or base-image mutation. No untraced effects accepted; missing licensed/accessible native targets block the evidence rather than justify infrastructure expansion.

**Representation contract:** Example-level native evidence on two amd64 OS builds (one Windows 11, one Server 2025), Go 1.26 and 1.27, repository x/sys. Socket domain: IPv4-only, IPv6-only, and a wildcard IPv6 socket accepting both IPv6 and mapped IPv4 traffic. The four traffic paths are IPv4-only, IPv6-only, native IPv6 on a dual-stack socket and mapped IPv4 on that dual-stack socket. Inspect binding/V6ONLY and capture cmsg level/type/length/int value for each receive path. For each send path, record the constructed CMSG level/type/length/native value alongside the independently observed peer mark, including mapped IPv4 on the dual-stack socket. Go net/Winsock and x/sys own the native ABI. Direct separate-family options must match the family actually receiving data; success on one family never substitutes for the other.

**Contract closure:** Not triggered. This evidence-only slice changes no runtime authority or lifecycle; ordinary focused native observations suffice. It does not claim exhaustive platform conformance.

**Evidence budget:** One finite pass per OS/toolchain cell, four cells total. In each, receive Not-ECT/ECT(0)/ECT(1)/CE from the independent peer and send Not-ECT/ECT(0)/ECT(1) to it over each of the four family paths; observe CE send rejection once per cell. Record both raw successful send counts and peer payloads, with and without ECN OOB. On each OS, using Go 1.27 only, check no-option/cleared-option metadata absence and disable one receive option at a time on the dual socket, plus URO enabled/disabled coexistence on one family; report actual coalescing separately. One decoder-bypass and one encoder-bypass experiment total, only if needed to establish probe discrimination; no further mutations, statistical repeats, throughput tests, kernel timing obligations or platform expansion. Existing W1 evidence may be cited but cannot replace the newly required Windows 11/dual-stack cells. One initial review and at most one replacement, per shared policy.

**Characterization first / preservation:** Build the native probe's bounded receive deadlines and independent peer observation before interpreting option success. Run the existing `TestWindows` group once per OS on Go 1.27 against the unchanged production baseline, recording existing gated skips without calling them passing engagement evidence. No repository Go implementation or dormant tests are required. Inspect the exact Go source for both toolchain lines only where differing send counts require explanation.

**Dispatch context budget:** At most 18,000 input tokens: this W2 contract and decisions, ADR 0007, program binding rules, W1 README and selected native result rows, `sys_conn_windows.go`, `managed_packet_receive_windows.go`, `external_packet_io.go:189-215`, `go.mod`, current Windows test names and only the applicable local native-host runbook. No complete earlier plan version or W1 troubleshooting transcript. Implementation here is disposable probing, leaving room for evidence review and correction in one fresh context.

**Slice decision audit:** Splitting by OS or by family would create independent receipts for one small qualification decision and duplicated probe mechanics; the bounded four-cell pass fits one context. Merging with W3 risks discovering a different ABI/send/family outcome after implementation has started and exceeds its implementation context. The W2→W3 edge is necessary because W3's Windows 11 and dual-stack claims depend on these unmeasured native facts, not merely convenient sequencing.

**Stop conditions:** Missing native access, a family or OS that cannot preserve the required marks, incompatible cmsg representation, ambiguous send success that cannot use the established native boundary, or evidence requiring a larger platform/NIC/provisioning project. Record the result and leave W3 blocked. Unsupported or contradictory evidence does not silently narrow W3 to one family, remove Windows 11, introduce a version gate, or mark W3 ready; return to scoped handoff for the changed outcome.

### Slice W3 — Enable managed Windows ECN through the exact lease

**Stable identity:** `QGF-ECN-20260922/W3`. One implementation PR; child [#538](https://github.com/the-sarge/quic-go-fast/issues/538).

**Status:** Ready — W2 and L1 are complete.

**What it delivers:** Both ECN directions through direct leases and synchronous exact-forwarding wrappers on the W2-qualified IPv4, IPv6 and dual-stack paths, including ordinary and batch output, filtering, fallback, reuse and cleanup. Capability becomes true only for the qualified endpoint and route.

**Existing-work disposition / blockers:** New slice retaining W1/W2 evidence and the merged L1 adapter, Windows datapath and batch normalization. Blocked by a successful W2 disposition confirming this contract. Read W2's merged receipt as the governing evidence; a contradictory result requires re-audit, not implementation guesswork.

**Single owner / authority completeness:** The endpoint owns native setup, qualification, storage, generation and cleanup. `windowsConn` owns Windows message representation and managed native send normalization. The existing adapter owns exact wrapper correlation; `WriteBatchV1` owns the checked singleton forwarding/result fact. Cover configuration, current/revoked leases, fallback and terminal Close in the same PR. No durable schema, restart format or new socket authority exists.

**Transitional seams:** Zero new duplicate state machines, temporary adapters, double opens or partial capabilities. W2 has no dormant production seam. Shared adapter build-constraint changes are mechanical reuse; Windows family setup remains separate from Unix syscalls. The final retained receiver and native send reference refer to the same endpoint-owned decoder/socket. No later slice is needed to remove temporary runtime behavior.

**Blast radius:** Windows decoder sizing, bounds validation, ECN representation and private send result handling; managed Windows setup/factory; Windows external ECN append; shared adapter build selection and associated tests. Port the platform-neutral guard fixtures from `managed_packet_ecn_unix_test.go` into shared Windows/Unix test coverage while keeping its x/sys/unix permission and oobConn decoder cases Unix-only; do not merely widen that whole test file’s build tag. Keep Linux/Darwin/FreeBSD semantics unchanged. Trace URO segment metadata, packet-info coexistence, full datagram borrowing, batch prefix/terminal errors, concurrent singleton association and close/rebind through existing owners. No public signature, wire format, dependency, process-global state or performance claim changes. Any required new cross-platform behavior or untraced lifetime effect stops this slice.

**Artifact classification:** Managed ECN is shipped behavior. Family gating, validated ancillary parsing and existing lease/correlation guards are required safety enforcement. Tests, native peers and receipts are verification aids or process metadata, not separately maintained platforms; no verification framework or harness completeness is a product gate.

**Representation contract:** Canonical-subset enforcement on endpoint-owned Winsock message I/O. The Windows parser validates header/data bounds and architecture alignment before access; ECN is absent or a single relevant native int32 in 0..3 at the W2-observed IP family level/type. Missing ECN is unsupported; truncated, malformed, duplicate or conflicting ECN is rejected before metadata can become authoritative. Well-formed unrelated ancillary messages retain their existing semantics. Sending accepts the internal protocol ECN enum, builds native int32 CMSGs using the established Windows layout and selects the socket/destination-family encoding from W2's successful send CMSG rows, including mapped IPv4 on dual-stack sockets. CE is a receive value and a native send rejection, not an invented success. Supported wrapper domain is ADR 0007's synchronous exact forwarding, with the existing generation/buffer/range/address and borrowed singleton identity checks. Universal gate statements apply only inside these mechanically checked domains; OS/network behavior is example-level qualification from the finite native matrix.

**Contract closure:** Triggered for the existing exact-lease correlation invariant because stale/wrong-peer metadata or misassociated send results can cross the accepted policy boundary through independently reachable direct, wrapper, batch and lease transitions. Keep its existing owners; the table below is the bounded Windows application, not a new general wrapper contract. Windows CMSG parsing and family qualification use focused tests of the canonical domain; no exhaustive external grammar claim.

| Semantic class | Disposition / enforcement owner | Evidence status and gate |
| --- | --- | --- |
| Current direct or exact wrapper read; selected vs rejected peer | Publish only final correlated read / shared adapter | Planned: native selected-peer fixture and inherited buffer/range/address checks |
| Missing, malformed or stale metadata; returned/replaced lease | Unsupported or reject before publication / Windows decoder then shared adapter | Planned: absent/invalid message table; lease return/reacquire regression |
| Direct native marked send | Preserve Windows success/error / Windows managed native mode | Planned: independent peer and zero/short/error result table |
| Checked singleton forwarding vs substituted payload/OOB/destination/result | Exact forwarding only / existing adapter and WriteBatchV1 operation | Planned: reuse inherited guard tests plus native checked-route case |
| Concurrent checked calls and terminal Close | Distinct operation association; revoke then join / adapter send mutex and endpoint lifecycle | Planned: bounded concurrency/close fixture and one race run |
| Registered general batch, partial progress and terminal failure | Preserve definite prefix / existing ordinary batch writer | Planned: existing progress tests plus Windows ECN batch peer observation |
| Unknown/buffering wrappers, raw handles, persistent state | Explicit non-goal | No new authority or evidence obligation |

**Evidence budget:** At most 12 new focused table-test functions, grouped by representation/setup, native routes, lifecycle and preservation; reuse existing adapter/batch/lifecycle tests. One positive per behavior and one negative per materially different failure mode. At most one deletion/bypass of the shared receive-correlation guard when inherited tests are the only evidence it protects Windows; no mutation quota on other owners and no recursive harness audit. Native managed smoke on both W2 OS builds using Go 1.27 for all four family paths (receive four marks, outgoing three permitted marks, direct and checked routes, ordinary and registered batch). CE input uses the independent Linux peer. Existing hosted Go 1.26/1.27 Windows unit jobs cover runtime-line regression without multiplying the native peer campaign. Run one native Windows race invocation for the focused managed group. No additional OS/architecture/NIC matrix, exact syscall-block timing, repeated stress loops or performance campaign. One initial review plus at most one replacement.

**TDD and preservation evidence:** First add a failing native direct/checked round-trip test and focused setup/parser/send-result tables. Extend in vertical steps through configuration, decoder, adapter and outgoing route. Include absent/invalid/truncated metadata followed by a valid packet; independent failure of each admitted family; opt-out with URO on/off; ECN-only full payload above protocol.MaxPacketBufferSize; lease return/reacquire without read-ahead; retained URO segment ECN/storage cleanup; concurrent singleton association; Close joining active read and send; unchanged callback result and terminal WSAEINVAL/WSAEMSGSIZE behavior; successful zero counts with and without pre-encoded ECN OOB and nonzero short-write rejection. Use existing tests where they discharge these obligations. No synchronization claim that a goroutine definitely reached a kernel syscall and no cancellation deadline SLA.

**Dispatch context budget:** At most 30,000 input tokens: this W3 contract/decisions, program binding rules, ADRs 0006/0007, W2 receipt (maximum 3,000 tokens; selected detail only), source manifest `managed_packet_endpoint.go`, `managed_packet_buffers.go`, `managed_packet_receive_windows.go`, `managed_packet_ecn_unix.go`, `external_packet_io.go`, `external_packet_io_oob.go`, `external_packet_io_no_oob.go`, `external_packet_io_batch_fallback.go`, `sys_conn_windows.go`, `coalesced_delivery.go`, and selected send routing in `send_conn.go`. Tests: relevant shared adapter guards, one BSD native fixture as a pattern, Windows packet-info/URO tests and endpoint lifecycle tests by named case, not all platform suites. Current source manifest is approximately 15,000 tokens before selected tests/contracts; stage reads to leave implementation/review capacity. If the required governing context exceeds the budget, re-audit before expanding the PR.

**Slice decision audit:** A native encoder/parser-only slice would ship dormant horizontal machinery; a receive-only slice cannot advertise the accepted capability. A single-family-first implementation could be independently useful but would add a temporary dual-stack rejection gate and repeat the same setup/parser/send owners after W2 has already resolved the family differences. A mechanical shared-adapter prefactor is small enough to remain inside this vertical PR. Thus retain one complete product slice with no successor dependency; split only if actual code evidence invalidates the declared context/blast-radius bound. Do not merge W2's unresolved native discovery into this implementation context.

**Stop conditions:** W2 does not confirm the representation; URO cannot preserve the accepted datagram/metadata contract through the existing owner; implementation needs public authority, a second lifecycle owner, a version gate, unregistered ECN enablement, dependency changes or broader send normalization; a supported family requires an unplanned representation; context or evidence cannot fit one PR. Apply the shared scoped re-audit/approach-stop policy rather than appending speculative cases.

## Acceptance criteria

- W2 records exact Windows 11 and Server 2025 builds, both supported Go lines, separate-family and dual-stack/mapped receive/send observations, constructed send CMSG level/type/length/native-value rows tied to independent outgoing marks, send counts, option-negative cases, URO coexistence and cleanup inside its finite budget. Production behavior remains unchanged.
- W2 marks W3 eligible only if the accepted native representation and every required family path are confirmed; otherwise it records the discrepancy and leaves W3 blocked for scoped handoff.
- W3 enables managed ECN only after both directions qualify for each admitted family and the exact direct/checked route qualifies. Failed admitted-family setup, unknown family, opt-out or unsupported wrapper retains usable ordinary I/O with ECN false.
- W3 preserves complete managed datagrams, selected-peer metadata association, ordinary/batch progress and native errors, concurrent singleton association, lease reuse and terminal cleanup through the existing owners. The named TDD cases and closure table discharge these obligations.
- W3 keeps GSO false for managed routes, projects actual URO state, preserves packet info and unregistered Windows behavior, and introduces no new public/raw authority or duplicate state machine.
- Native runtime claims are example-level on the recorded W2 targets; runtime input guarantees are the W3 mechanically enforced canonical subset. No criterion extends to older releases, arbitrary wrappers or an exhaustive offload matrix.

## Validation gates

W2 uses its bounded native commands/receipt plus docs-only exact-head certification under the repository overlay. W3 runs `go test -count=1 .`, `go vet .` and `go mod tidy -diff` on changed Go code, plus the new focused Windows group and existing `TestWindows` group natively on the two targets. Run the focused race group once on Windows; existing hosted platform/cross-compile jobs provide the shared-build regression boundary. Native family tests may not skip a required W2-qualified path; existing optional hardware-engagement tests may retain explicit skips. Record source SHA, commands, native OS/toolchain identity and actual results. Require applicable hosted checks at the exact final head. No full native cross-product beyond the stated slice budget.

## Operating discipline

The active skills' shared review-loop (`_shared/REVIEW-LOOP.md`) and contract-closure (`_shared/CONTRACT-CLOSURE.md`) baselines govern, composed with the repository [execution overlay](../REVIEW-LOOP.md), ADRs 0006/0007 and [maintained-code conventions](../agents/conventions.md). Resolve shared baselines through the active skill installation; no repository copies are introduced.

Each slice runs through `$implement-architecture-slice`: preflight → bounded manual review and dispositions → exact-head local certification and hosted checks → squash merge → append-dev-journal → reconcile current child/frontier pointers → complete the OmniFocus slice task. A successful W2 may advance W3 administratively without changing its accepted contract; any different representation, topology, outcome, adjacent scope or one-PR boundary requires scoped handoff. This handoff creates no implementation dispatch.
