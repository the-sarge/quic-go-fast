# Captured Initial starvation in #46

## Finding

Both retained failures show the client missing the server's entire Initial CRYPTO flight: each server Initial carrying CRYPTO was changed by the test callback, written successfully, received byte-for-byte as changed, and rejected for failed payload authentication. The Initial packets the client processed carried only ACKs. It never derived Handshake keys. This directly explains the stalled handshake in these two occurrences. It does not establish the cause of every earlier unrecorded #46 timeout or universally exclude contributions from other changes.

The defect in the mandatory fixture contract is unconditional success under a permitted random sequence that can destroy every usable handshake attempt before its deadline. The accepted correction is documented in [the current contract](../../agents/corruption-ci.md): deterministic recovery and starvation cases, with original random corruption retained as explicit opt-in exploration. Production recovery and deadlines are unchanged.

## Provenance

| Capture | Source and command | Environment | SHA-256 |
| --- | --- | --- | --- |
| [I1](raw/i1.jsonl) | `76c6fb97077a4b143fe05212de5bfa484f32ae94`; `go test ./integrationtests/self -count=1 -version=2`; [receipt](raw/i1-receipt.json) and [issue receipt](https://github.com/the-sarge/quic-go-fast/issues/46#issuecomment-5613520556) | macOS arm64, Go 1.27.0, no race, QUIC v2, scale 1 | `1ed6e68e39353bd346f65063b5f77820e4fc3f56839725bd41cbf55cf97a19a8` |
| [I2](raw/i2.jsonl) | PR #156 head `5c231567aa02524fa51995abc5c62269888f8774`, base `8cba598931f38814ba8a81cd7fccbaaf0bbdfca4`; tested GitHub merge commit `2a4f2c565c8880c5006f60922a089cdb0adad65b`; `go test -race -v -timeout 5m -shuffle=on ./integrationtests/self -version=1` | Linux amd64, Go 1.27.1, race, QUIC v1, scale 3, GSO not explicitly disabled | `53b032d4075edbc51367079de91e44de3f00500082b1307725a1afcba770c7fc` |

I2 [failed job](https://github.com/the-sarge/quic-go-fast/actions/runs/34443639515/job/102763646487), [PR receipt](https://github.com/the-sarge/quic-go-fast/pull/156#issuecomment-5614017807). The merge commit's parents were verified against the stated base and candidate; this accounts for the different `GITHUB_SHA` in capture metadata. Both files have a complete Dial terminal record, zero reported dropped/truncated records, and valid encoded qlog records. They contain 201 and 311 records respectively. Original full logs remain at `/Volumes/worktrees/quic-go-fast/i1-ci-evidence/loss-integrated-v2.log` and `/Volumes/worktrees/quic-go-fast/i2-evidence/ci-failure.log`.

## Causal chain

Line numbers below refer to the retained JSONL, not observation ordering. Producers can append timestamps out of order. [analyze.py](analyze.py) checks raw hashes, reconstructs the byte replacement, compares full modified payloads with socket write/read bytes, and correlates the authentication drop. [summary.json](summary.json) records the complete derived chain and server timer events. Run `python3 docs/audits/issue-46-captured-corruption/analyze.py` for offline reconstruction; this sends no traffic.

| Capture / Initial packet number | Sent line | Mutation line | Socket write/read lines | Authentication rejection line |
| --- | --- | --- | --- | --- |
| I1 / 0 | 43 | 55 | 54, 59 | 60 |
| I1 / 2 | 105 | 113 | 112, 119 | 122 |
| I1 / 3 | 107 | 117 | 116, 121 | 123 |
| I2 / 0 | 43 | 57 | 54, 55 | 61 |
| I2 / 3 | 106 | 116 | 113, 115 | 119 |
| I2 / 4 | 108 | 123 | 121, 122 | 124 |
| I2 / 8 | 224 | 232 | 231, 236 | 240 |
| I2 / 9 | 226 | 239 | 238, 241 | 242 |

I1 processed two ACK-only server Initials; I2 processed six. Successful ACK reception does not supply the missing server Initial CRYPTO. The buffered Handshake/1-RTT packets cannot remedy missing Handshake keys. Immediate replacement delivery bypasses the ordinary proxy delay and has the original endpoint source address; the socket observations retain that distinction. No selected replacement short write or socket error explains these eight losses.

## Recovery timing check

At pinned implementation `8cba5989`, `internal/utils/rtt_stats.go` uses a 200 ms PTO before the first RTT measurement. `sent_packet_handler.getScaledPTO` shifts that duration by `ptoCount`; `getPTOTimeAndSpace` selects the earliest outstanding Initial or Handshake timer using each space's latest ack-eliciting send. `OnLossDetectionTimeout` increments the shared count and requests two probes. I2 does not change those functions. These rules explain the observed alternation: Initial probes near 0.217 s, Handshake near 0.418 s, Initial near 1.018 s and Handshake near 2.019 s after capture start. Each expiry is accompanied by two packet submissions in its selected space.

I2's final recorded next Initial timer is `06:06:09.694593001Z + 2198.305202 ms`, approximately `06:06:11.892898203Z`, later than the Dial deadline `06:06:10.675905821Z`. I1's final Initial timer is `00:49:39.154957-04:00 + 596.734375 ms`, approximately `00:49:39.751691375-04:00`, later than its deadline `00:49:39.681631-04:00`. Neither trace requires an unobserved lost timer to explain the deadline.

The timer-space selection and shared exponential backoff agree with [RFC 9002 §6.2.1](https://www.rfc-editor.org/rfc/rfc9002.html#section-6.2.1); the probe submissions are consistent with [§6.2.4](https://www.rfc-editor.org/rfc/rfc9002.html#section-6.2.4). This is a check of this observed recovery path, not a protocol-wide conformance audit. No production defect was demonstrated by these captures.

## Regression interpretation

The three new simulated cases distinguish persistent Initial CRYPTO starvation from allowing the first retransmission and undamaged operation. The existing finite Initial/Handshake recovery and sustained Handshake-damage cases remain. Mandatory real UDP coverage uses explicit finite mutations followed by reliable delivery in both directions. Authentication drops and mutation/release observations prevent a passing case from silently bypassing its intervention.

Live encrypted bytes, packet numbers, exact mutation offsets and elapsed timestamps remain historical evidence. Fresh TLS packetization is not identical, so the maintained regression uses frame semantics and public completion/data/error results. The original capture added synchronous encoding/file I/O and affected scheduling; this remains a limit on timing inference. No new randomized attempt or hosted rerun was used to derive the finding. Development and certification are recorded separately in the PR's validation receipt.
