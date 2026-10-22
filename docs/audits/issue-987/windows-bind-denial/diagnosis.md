# Native TCP bind-denial recurrence

## Observed mechanism

After the initial inconclusive investigation and diagnostic merge [#991](https://github.com/the-sarge/quic-go-fast/pull/991), the journal PR's normal [Windows Go 1.27.3 unit job](https://github.com/the-sarge/quic-go-fast/actions/runs/39828204122/job/119997028633) reproduced the same setup exhaustion. This was a natural required CI run, not a requested unchanged-head rerun or added investigation campaign.

All ten UDP-selected loopback ports, 49862 through 49871, failed TCP `listen` with `*net.OpError` wrapping `*os.SyscallError` and `syscall.Errno(0x271d)` (10013): "An attempt was made to access a socket in a way forbidden by its access permissions." The helper never ran. This establishes the fixture mechanism: a UDP-assigned ephemeral port does not establish TCP bind eligibility, and ten adjacent UDP candidates can all be denied by TCP. It does not establish which excluded range, exclusive binding, reservation or security policy caused the denial on that expired runner, nor prove the uninstrumented historical job had precisely the same OS error.

The corrective decision is fixture-local: ask TCP's allocator for a usable ephemeral listener first, keep that reservation held, and probe UDP at its actual address. Close UDP before calling the unchanged helper; only the helper must open UDP before TCP. If UDP cannot bind, close that TCP candidate and try again within the existing ten-attempt budget. Immediate per-resource cleanup registration covers assertion exits, including a failed immediate close. Keep error-chain diagnostics for both allocation stages and preserve the TCP `listen` error, unused hooks and UDP rebind assertions.

The fixture owns its socket preconditions synchronously. The alternative of changing the production listener owner is outside scope; revisit only on demonstrated production failure and approved expanded scope. More retries or Windows skips are rejected on merit. This correction avoids the demonstrated invalid inference without claiming all TCP/UDP allocations can always succeed.

## Evidence and limits

The job ran journal head `f530bd9195a8c64a6b841f566819419223f4aa45` through GitHub's synthetic PR merge `dac36617e6ab45dd7259a9980ff77a38bd079aad` (tree `9b20b69408d42afc0f54d3e139f154d4496c81b7`) on Windows Server 2025, build 26100, AMD64, Go 1.27.3. Command: `go test -v -shuffle on -cover -coverprofile coverage.txt ./...`; `GOTOOLCHAIN=local`, `TIMESCALE_FACTOR=10`, HTTP/3 shuffle seed `1792692485838992000`. The only fixture change from main is none: the journal head adds documentation.

All four retained wrapper checksums validated. `result.json` records exit 1, 667242 observed/retained output bytes, no truncation and no capture errors. `source.patch` is empty; provenance is unit-scoped with `integrationtests` excluded and `source_complete=false` because `report.xml` was untracked. Do not call this a complete source archive. The [failure archive](failure-evidence.tar.gz) preserves exact wrapper metadata/logs/checksums and the full job log; its [checksum](SHA256SUMS) binds retained bytes.

Protocol-specific excluded/reserved TCP ranges are a plausible underlying cause: their prediction is that the failed addresses overlap a TCP restriction absent from UDP. Existing exclusive TCP ownership predicts conflicting TCP ownership on those addresses. Broad TCP access denial predicts that TCP port-zero allocation also fails. The failed runner is gone, so the first two cannot be distinguished from retained evidence. The selected correction enforces TCP eligibility directly instead of choosing among those unproven host causes. Any configuration captured on validation's new runner describes that runner only.

## Terminating validation

Use only the already allocated remaining native validation job, at most 30 minutes including setup: 100 focused executions with fail-fast and a 60-second Go timeout; combined lifecycle tests once; HTTP/3 with the recurrence's captured seed once (five-minute stage cap); unit scope once (ten-minute stage cap). Retain actual source, image/build, commands, error evidence and checksums. Local focused/lifecycle race/package tests, vet, module tidiness, lint and final exact-head PR CI remain required. No second reproduction campaign or unchanged failing-head rerun is allocated. Validation results will be recorded before the correction candidate is reviewed.
