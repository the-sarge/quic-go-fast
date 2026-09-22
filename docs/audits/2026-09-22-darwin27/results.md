# Darwin 27 arm64 qualification: inconclusive

The bounded collection passed its executable checks but cannot qualify runtime admission: source applicable to XNU 13432.1.9 was not found in the captured Apple release-tag inventory, and this run does not establish target-kernel suppression of EAGAIN, EINTR or ENOBUFS after partial progress. Darwin 27 remains unqualified on both arm64 and amd64. No production admission entry was added, and no collection was repeated to obtain a pass. Issue [#516](https://github.com/the-sarge/quic-go-fast/issues/516) remains open for the missing semantic evidence.

## Reproducible identity

The [protocol](protocol.md) was committed at `c6839da74315561bd254658daf51d8308232f0f9` before collection. The prepared, tracked-clean candidate was `129ebfc835eb4cb5e918f7d7726a17326b48f322`, based on `67c5232464364e6d453fb41f5d51dd4f54d600cd`. [identity.json](identity.json) records the collection timestamp, exact host/toolchain and SHA-256 hashes of original source, replacement source and overlay. The host was macOS 27.0 build 26A428, Darwin 27.0.0, XNU 13432.1.9~1/RELEASE_ARM64_T6041, arm64, Go 1.27.1.

The archived [replacement source](admission.go.txt) differs from the candidate's `sys_conn_sendmsg_x_qual_darwin.go` only by the test-only Darwin 27 arm64 allowlist entry. [overlay.json](overlay.json) records the absolute paths used. The real host identity, production self-check, syscall adapter, counters and error handling were retained; no capability flag was forced. The overlay is retired from execution after this collection and retained as frozen evidence. To inspect or reproduce the historical run, check out the candidate and remap the overlay paths to the exact archived replacement bytes; rerunning is not part of this qualification.

## Executed evidence

[commands.json](commands.json) records each command, environment override, exit code and duration. Every recorded command returned zero. The case summaries distinguish test/subtest events from the final package event.

| Gate | Result | Recorded output |
| --- | --- | --- |
| Focused native matrix | 196 passing test/subtest events, zero skips; package passed | [case inventory](focused-cases.json), [full JSON stream](focused.jsonl.gz) |
| Root package | 1453 passing test/subtest events, seven unrelated platform skips; package passed | [summary](package-cases.json), [full JSON stream](package.jsonl.gz) |
| Root race gate | Same counts; no race reports | [summary](race-cases.json), [full JSON stream](race.jsonl.gz) |
| Private-syscall opt-out delivery | Three passing tests, zero skips; package passed | [summary](opt-out-cases.json), [full JSON stream](opt-out.jsonl.gz) |
| Darwin arm64 and amd64 | Ordinary and opt-out builds passed | Command manifest; empty build logs record no output |
| iOS arm64 | Ordinary and opt-out library builds passed; active private-syscall files excluded | [build log](ios-build.log), [selected files](ios-files.log) |
| Native opt-out file selection | Active private-syscall files excluded | [selected files](opt-out-files.log) |
| Local static checks | go vet, go mod tidy -diff and diff whitespace checks passed | Command manifest |
| Architecture guard sensitivity | Bypassing only the architecture guard made the table test fail for Darwin 27/amd64 | [mutation receipt](admission-mutation.log) |

The package/race skips are TestConnectionStatePathCapabilities, TestManagedPacketECNRejectsStaleGeneration/lease_close_joins_correlation, TestSendConnDetectGSOFailure, TestSendConnGSOFallbackConcurrentCapabilities, TestSendConnSendmsgFailures, TestOOBReaderBatchProgressionAndCleanup and TestSysConnSendGSO. These are existing non-Darwin/GSO-specific cases; none is a required native case in this protocol. The focused run had no skips.

## Semantic disposition

| Accepted class | Observed evidence | Disposition |
| --- | --- | --- |
| Admission | Darwin 25 arm64/amd64 accepted by finite table; test-only 27 arm64 accepted, 27 amd64 and unlisted majors rejected; excluded-architecture integration delivered via ordinary writes with no native submission | Covered; production 27 entry absent |
| ABI, destination representation and startup | Layout and sockaddr tests passed; actual production self-check asserted success | Covered on declared host |
| Families, payloads and ECN | IPv4-only, IPv6-only, mapped IPv4 and native IPv6 dual-stack cases asserted exact payloads/ECT0 and increased batch-submission counters; managed receive/send-route cases passed | Native example-level coverage |
| Ordinary, fixed-peer, external writer, forwarding and lifecycle | End-to-end and authority tests passed; deadline, closed-socket, concurrent writer and native lease reacquisition assertions passed | Covered within protocol |
| Native oversized first entry | Two offered entries returned accepted=-1, errno=40 (EMSGSIZE); peer had no queued datagram | Native zero-progress EMSGSIZE observation |
| Native accepted prefix before oversize | Three offered entries returned accepted=1, errno=0; peer received only the exact prefix with ECT0 | Native partial-count EMSGSIZE observation |
| Worker attribution and retry | Existing native external message-size feedback and injected send-worker full/partial/unknown/invalid-progress tests passed | Native and injected evidence distinguished |
| Unavailable syscall and invalid results | ENOSYS and invalid count injection latched off; injected unknown progress was fatal without resend; injected zero-progress errno tests passed | Caller behavior only, not target-kernel semantics |
| EAGAIN, EINTR and ENOBUFS after progress | No applicable target-source mapping or direct native observation in this run | Missing; qualification inconclusive |
| Disabled/unqualified/opt-out fallback | Exact ordinary delivery; disabled/unqualified cases asserted no native submission; opt-out builds omit active code | Covered |
| Other architectures and future kernels | Cross-builds only; no native amd64 qualification or claim about later kernels | Explicit non-goal |

## Source boundary and remaining work

The [Apple XNU repository](https://github.com/apple-oss-distributions/xnu) release-tag inventory was captured with `gh api repos/apple-oss-distributions/xnu/tags --paginate --jq '.[].name'`; its exact output is [apple-xnu-tags.txt](apple-xnu-tags.txt). It contains no `xnu-13432.1.9` tag. This is evidence of a missing published revision in that inventory, not a claim that no other applicable source can exist. No Darwin 25 or unrelated latest-source semantics were substituted. The EMSGSIZE observations establish only the two recorded cases and do not generalize to EAGAIN, EINTR or ENOBUFS.

A future attempt needs source applicable to the running target revision, with a defensible version mapping, or bounded direct evidence for the unresolved assumptions. It must explicitly revise the protocol, retain this inconclusive result and declare any needed revalidation. This PR does not change errno interpretation, retry behavior or datapath performance. The architecture-aware predicate and focused regressions prepare future evidence-matched admission while preserving current fallback.

Original D1 protocols, results, collectors and prior managed ECN evidence are unchanged. Review and exact-final-head certification receipts belong in the PR discussion; they do not replace or relabel this collection's candidate identity.
