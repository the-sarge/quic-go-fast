# Distributed startup fixture port selection

The operator approved a separate fixture-only prerequisite to #971 after its [Windows Go 1.26.10 unit job](https://github.com/the-sarge/quic-go-fast/actions/runs/39918053217/job/120296276491) failed `TestListenAndServeTLSStartupFailureReleasesUDP`. The [execution contract](../../agents/windows-startup-port-selection-1002.md) bounds this work. Production server/recovery behavior and frozen #987 evidence remain unchanged.

## Failure and causal decision

At candidate `486195e5384c83b4b3dc14f48de082bb4ed1cf08`, every TCP allocation on ports 63336–63345 succeeded, while every UDP probe on those addresses failed with `*os.SyscallError` wrapping WSAEACCES/10013 (`syscall.Errno(0x271d)`). The helper never started. This was the only failed test; the recovery package and new adaptive-loss regressions passed. The raw [failure archive](failure-evidence.tar.gz) retains the full job log, whose SHA-256 is `98129079f171ad1ccbb5d4bbc9f735c2f6509ba1bf8f2911867e2a044d8a0774`. No unchanged-head rerun was requested.

The previous #987 correction observed UDP-selected ports denied for TCP. Together these failures show that changing which protocol allocates port zero does not make ten adjacent allocator choices independent of protocol-specific restrictions. The exact restriction or ownership behind the expired failing runner's denial remains unknown. Broader UDP failure would also defeat other candidate addresses; this cannot be established or excluded retrospectively from that log.

Select up to ten independent candidate numbers in 1024–65535, reserve TCP and then probe UDP at each actual address. Reject and release unavailable candidates. Keep successful TCP held, close its UDP probe and invoke the unchanged UDP-first helper. This removes dependency on a protocol allocator's sequential range while retaining actual bind validation, bounded attempts, typed diagnostics and cleanup. Returning to UDP-first selection merely swaps the previously demonstrated failure directions; increasing retries, Windows skips, global network configuration changes and production injection seams were rejected on merit.

## Native validation

The sole allocated [Windows validation run](https://github.com/the-sarge/quic-go-fast/actions/runs/39927044458), attempt 1, passed on `93b3ad9ce5047c9bf4b36535bbcca4ead5f89ed5`, Windows Server 2025/build 26100/AMD64, image `win25-vs2026` version `20261007.264.1`, Go 1.26.10, `GOTOOLCHAIN=local`, `TIMESCALE_FACTOR=10`. The complete job budget was 30 minutes; no second native investigation job is allocated. All three stages exited zero:

| Stage | Exact command | Fixture passes | Retained output bytes |
| --- | --- | --- | --- |
| Focused | `go test -v -count=100 -failfast -timeout=60s -run ^TestListenAndServeTLSStartupFailureReleasesUDP$ ./http3` | 100 | 31,429 |
| Combined lifecycle | `go test -v -count=1 -timeout=2m -run ^TestListenAndServeTLS ./http3` | 1 | 1,302 |
| Captured HTTP/3 order | `go test -v -count=1 -shuffle=1792740625500411700 -cover -coverprofile=coverage-http3.txt -timeout=5m ./http3` | 1 | 93,146 |

The focused run encountered an actual denied TCP candidate at 49723, within that runner's recorded TCP exclusion 49688–49787, then succeeded on its second candidate at 21151 for both protocols. It retained the real helper's TCP conflict, unused hooks and UDP rebind assertions. This directly exercises the newly admitted TCP-candidate rejection path. The current UDP exclusion ranges were 52578–52677 and 52678–52777, distinct from TCP's exclusions. Both dynamic ranges were 49152–65535. These observations describe this validation runner and do not identify the old runner's policy. Range inspection used the read-only commands in [Microsoft's netsh interface documentation](https://learn.microsoft.com/windows-server/networking/technologies/netsh/netsh-interface-portproxy).

The [native archive](native-validation.tar.gz) retains all three command wrappers' `run.json`, `result.json`, `output.log`, empty `source.patch` and `SHA256SUMS`, contemporaneous environment/range output and the full job log. All twelve wrapper checksums were independently verified; outputs are untruncated with no capture errors. Each stage records a clean pinned checkout and `source_complete=true` within whole-checkout provenance scope. This archive contains provenance and logs, not a complete source archive. [Archive checksums](SHA256SUMS) bind the retained bytes.

The temporary validation workflow is removed before the final candidate. The fixture blob is unchanged from the natively validated revision: `9f73d34008693ff22486f9a0f888a3dd5b42a9b9`. Final local exact-head and hosted PR checks are separate receipts in PR discussion. Finite passes and observed rejection recovery do not establish universal port availability or a statistical flake-rate guarantee.
