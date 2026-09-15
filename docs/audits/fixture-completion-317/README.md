# TestListenAddr fixture completion: confirmation and repair

## Result

The explicit `TestListenAddr` → `TestDial/Dial` sequence reproduced the existing `client_test.go:104` assertion on repetition 928, without lifecycle or observer instrumentation. Adding only a join of the earlier listener fixture's own receive-completion channel produced 1,000 passing repetitions. The fixture-boundary regression failed on its first execution without that join and passed 1,000 times with it. These observations satisfy the [approved example-level contract](../../agents/fixture-completion-317.md); they do not prove universal absence of transport overlap or the identity of the original hosted failure's uncaptured transport.

The maintained repair is confined to `server_test.go`: `newTestListenAddr` constructs a real listener and registers cleanup that closes the listener and joins its existing `Transport.listening` channel. `TestListenAddr` retains its address-error assertions and uses that fixture. `TestListenAddrFixtureStopsTransport` creates the same fixture in a child test and checks completion immediately after the child's cleanup returns. The one-second scaled watchdog fails stalled cleanup; it does not replace the channel join with a timed delay.

Production source, `TestDial`, and the global goroutine observer are unchanged. Public listener semantics, caller-owned sockets, and incoming-storage ownership are unchanged. The temporary sequence test was removed after confirmation. #241 and the HTTP investigations remain separate. The [earlier diagnosis](../dial-317/README.md), its exhausted budget, and PR #316's historical failure/merge exception remain preserved.

## Four-launch evidence

| Launch | Source and purpose | Executed repetitions | Result |
| --- | --- | --- | --- |
| E1 | Baseline plus temporary explicit sequence; original tests/assertions. | 928: 927 passed, then target failure. | Exit 1; `client_test.go:104` expected false. Root test time 0.955s. |
| E2 | Same sequence with only `TestListenAddr`'s receive-completion join added. | 1,000 passed. | Exit 0; root test time 1.038s. |
| E3 | Shared real-listener fixture and completion regression, join absent. | First regression failed after child fixture passed. | Exit 1; fixture returned before receive completion. Root test time 0.069s. |
| E4 | Same fixture/regression with join restored. | 1,000 passed. | Exit 0; root test time 0.438s. |

All four authorized slots were used. E3 is the discriminating guard-omission control; no additional mutation or repetition campaign was run. Go times above exclude compilation and downloads. The full commands, timestamps, statuses, source patches, and compressed logs are retained here. `e1.patch` is intentionally empty: that attempt adds only [sequence.go.txt](sequence.go.txt) as `fixture_sequence_317_test.go`. [launches.md](launches.md) explains the source composition for each launch. None used the archived diagnostic patches from the previous investigation.

The Go command in each launch used coverage, `-count=1000 -failfast -timeout=9m`, seed `1789428569339240250`, and an outer `timeout --signal=TERM --kill-after=5 595`. E1/E2 selected `^TestFixtureSequence317$/^(TestListenAddr|TestDial)$/^Dial$`; E3/E4 selected `^TestListenAddrFixtureStopsTransport$`. The temporary parent explicitly calls the listener test before the dial test, and the logs verify that order. The second selection excludes the other dial variants only in this temporary experiment; maintained `TestDial` is untouched and remains part of normal root-package validation.

## Environment and limitations

Baseline `555b52fb22c73a59819357b8e13e2d18631506aa` differs from planning/diagnosis source `4fcf07fd9836a6dafebfde7bbbf21fef436d2dc7` only in documentation/evidence. The worktree is `/Volumes/worktrees/quic-go-fast/fixture-completion-317`, branch `codex/fixture-completion-317`. Existing frozen evidence was not edited.

The configured native amd64 environment was unavailable: SSH to `dev-agent-imacpro` exited 255 because its Lima instance did not exist. No infrastructure was started or provisioned. The existing Docker image `sha256:20dfa9aeeb42795279e3e73318bdd425f1fe09e3cebed05484fce1a3ca9aef20` supplied Ubuntu 24.04.4, Go 1.26.8 linux/amd64, `GOAMD64=v1`, CGO enabled, `GOTOOLCHAIN=local`, `TIMESCALE_FACTOR=10`, UID 1001, default GOMAXPROCS, and LinuxKit 7.0.12. Execution was emulated on an ARM Mac. This differs from the original hosted Ubuntu 24.04.5 / Linux 6.17 environment and does not reproduce its scheduling or load.

Each container mounted source read-only, copied it into a private runtime directory, and used private writable caches. Source copy and environment inspection preceded the bounded Go command; every observed launch completed within ten minutes. Container launches were non-race, and added no transport identity labels, profile captures, sleeps, or allocator instrumentation. The explicit nesting and extra regression assertions still affect scheduling. The finite passing counts establish the observed intervention/regression results, not statistical flake-rate bounds.

## Retention and subsequent validation

This directory is frozen confirmation evidence, not a maintained harness. [SHA256SUMS](SHA256SUMS) covers the archived source, scripts, and experiment receipts/logs. Original uncompressed copies remain at `/Users/josh/diagnostics/fixture-completion-317-2026-09-15/`. The normative plan owns the evidence budget and stopping rules; PR review/local-certification/hosted-check receipts are recorded separately in the PR discussion. Those delivery gates must pass on the exact candidate before merge.
