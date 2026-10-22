# Windows combined-server startup fixture diagnosis #987

## Result

The bounded native investigation did not reproduce the reported setup failure. No failed TCP setup bind was observed, so the original cause remains unknown and no port-selection remedy is justified. Keep [#987](https://github.com/the-sarge/quic-go-fast/issues/987) open. The delivered change retains actionable future diagnostics and registers cleanup before assertions can abort; it does not claim a fixed flake.

The [approved contract](../../agents/windows-startup-fixture-987.md) bounds this investigation. The source baseline is `b65664cb0ef138de987bd4532efe83e933767ec6`; the instrumented revision is `690ad31067345535bfe78ab01e1a3969602aea14`. The original allocation sequence and ten-attempt limit are unchanged. Production `listenAndServeTLS` is unchanged: it opens UDP, defers UDP closure, then attempts TCP before running either hook.

## Historical evidence and limits

The [original Windows job](https://github.com/the-sarge/quic-go-fast/actions/runs/39804117693/job/119912628339) failed after ten setup attempts before the helper ran. Its discarded bind errors prevent distinguishing port occupancy, excluded/reserved ports, permissions, or another cause. The [triage brief](https://github.com/the-sarge/quic-go-fast/issues/987#issuecomment-6347102187) records the retained original artifact and its completeness limits. Neither the independent passing PR matrix nor the passes below explains that failure. This is not demonstrated production failure or demonstrated external infrastructure failure.

## Native Windows evidence

The sole diagnostic [run 39826377537](https://github.com/the-sarge/quic-go-fast/actions/runs/39826377537), job `119990637773`, attempt 1, executed the pinned instrumented revision on native Windows Server 2025, build 26100, AMD64, with Go 1.27.3, `GOTOOLCHAIN=local` and `TIMESCALE_FACTOR=10`. The complete job log records the actual runner image/version. All three commands exited zero:

| Stage | Exact Go command | Outcome | Retained output bytes |
| --- | --- | --- | --- |
| Focused | `go test -v -count=100 -failfast -timeout=60s -run ^TestListenAndServeTLSStartupFailureReleasesUDP$ ./http3` | 100 startup-fixture pass(es); no failed setup bind | 21550 |
| Captured order | `go test -v -count=1 -shuffle=1792682603248206600 -cover -coverprofile=coverage-http3.txt -timeout=5m ./http3` | 1 startup-fixture pass(es); no failed setup bind | 93053 |
| Unit scope | `go test -v -count=1 -shuffle=on -cover -coverprofile=coverage.txt -timeout=10m ./...` | 1 startup-fixture pass(es); no failed setup bind | 662091 |

The focused stage was budgeted for at most 100 executions with fail-fast and a 60-second Go test timeout. Subsequent stages were admitted only because it did not reproduce the failure. The captured-order stage had a five-minute cap; unit scope had a ten-minute cap; the complete job had a 30-minute cap including setup. No second investigation/validation job, unchanged failing-head rerun, statistical flake-rate claim or extra campaign was used. The unit HTTP/3 shuffle seed was `1792691609792617900`.

Every observed selection succeeded on its first attempt. The unit scope removes `integrationtests` exactly as the existing unit workflow does; the explicit uncached count and timeout caps are recorded command differences from the historical invocation. The current HTTP/3 test set also differs from the old candidate, so replaying its seed is a bounded ordering probe, not an exact replay of the historical machine or suite state.

The [native archive](evidence/native-windows.tar.gz) retains the command wrapper's original `run.json`, `result.json`, `output.log`, empty `source.patch` and `SHA256SUMS` for each stage, plus the full job log. All twelve wrapper file checksums validated after download. All results report complete untruncated output and no capture errors. Focused/order source provenance is whole-checkout scope; unit source provenance is explicitly unit scope with `integrationtests` excluded. Its `source_complete=true` applies to that scope; this archive contains provenance and logs, not a complete source archive. The [archive checksums](evidence/SHA256SUMS) cover exact retained artifact bytes.

## Controlled diagnostic probe

On Go 1.27.3, darwin/arm64, a temporary real TCP listener held each selected address before the fixture attempted its own TCP reservation. The named test failed as intended after exactly ten attempts. The [probe archive](evidence/controlled-exhaustion.tar.gz) contains the exact patch and verbatim output showing selected addresses, complete bind errors, `*os.SyscallError` and `syscall.Errno` (`0x30`, address already in use). The probe ran once with `go test -v -count=1 -timeout=60s -run '^TestListenAndServeTLSStartupFailureReleasesUDP$' ./http3` against the instrumented revision plus only that patch, which was removed immediately afterward. This demonstrates diagnostics, not reproduction of the original Windows failure. Cleanup is registered immediately after every successful acquisition, before a fatal assertion can occur; temporary UDP sockets still close before the helper and the successful reservation stays held through it.

## Local checks and remaining evidence

On Go 1.27.3, darwin/arm64, the focused test, `go test -count=1 -race -timeout=2m -run '^TestListenAndServeTLS' ./http3`, `go test -count=1 -timeout=5m ./http3`, `go vet ./http3`, `go mod tidy -diff`, golangci-lint 2.14.0 for `./http3/...`, formatting, diff checks and temporary-workflow actionlint passed. These runs apply to the instrumented fixture; final candidate/base certification and hosted PR checks are retained in PR discussion rather than added to this historical record.

The next useful evidence is a native Windows setup exhaustion with the retained attempt addresses and typed error chain/OS code, pinned revision, exact command, seed, actual image/build and scope/completeness metadata. If that error implicates address occupancy or exclusion, collect contemporaneous listener ownership and the relevant TCP/UDP dynamic/excluded-port configuration on that failing runner before selecting a remedy. No additional investigation is authorized by this report. A future failure now supplies the error evidence the original report lacks.
