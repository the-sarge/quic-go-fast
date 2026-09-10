# Development execution ledger

Base `8cba5989`, Go 1.27.0 macOS arm64. Logs are adjacent. This ledger counts focused development command attempts, including the build failure, against the approved 12-execution budget. No random stress ran.

| Execution | Selected tests (`go test ./integrationtests/self -count=1 -run ...`) | Expected signal and outcome |
| --- | --- | --- |
| 01 | `^TestCorruptionRandomOptIn$` | Red: missing explicit stress entrypoint cannot satisfy subprocess routing assertions. Failed as expected; no random leaf selected. |
| 02 | `^TestCorruptionRandomOptIn$` | Green: default skip and explicit enable are observed without leaf execution. Passed. |
| 03 | `^TestCorruptionFiniteCallback$` | Build failed: internal long-header packet enum has no 1-RTT member. Changed the fixture's selection representation to existing `qlog.PacketType`, which names all three supported packet kinds. No behavioral execution. |
| 04 | `^TestCorruptionFiniteCallback$` | Red: the original random callback does not implement finite selection. The injected non-selection draw made each selected-packet assertion fail. |
| 05 | `^(TestMITCorruptPackets\|TestCorruptionFiniteCallback\|TestCorruptionDiagnosticsCallback)$` | Green: six real UDP recovery cases, finite callback byte/forwarding assertions and preserved random callback behavior. Passed. |
| 06 | `^TestHandshakeCapturedCorruption$` | Red: reliable link cannot satisfy expected starvation; release control cannot claim an intervention. Also exposed that short-header qlog events can omit their optional checksum. Length mapping remains authoritative in the ordered simulated domain; compare checksums when present. |
| 07 | `^TestHandshakeCapturedCorruption$` (`-v`) | Green after introducing actual protected-byte corruption: starvation has three rejected Initial CRYPTO attempts, six accepted Initial ACKs, no received Initial CRYPTO or Handshake keys; release and undamaged controls complete. Passed. |

Usage: 7/12 focused development command attempts. Final certification and any accepted correction evidence are recorded in the PR receipt, separately from this historical red/green sequence. The unimplemented mutation in execution 06 was part of the test-first cycle; no standalone mutation campaign was run.
