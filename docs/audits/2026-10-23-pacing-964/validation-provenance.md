# Local validation provenance

These invocations are reconstructed from the implementing agent’s retained execution-tool transcript, rather than from command headers embedded in the files. The Go logs contain bare package-success output; they independently show package-level success, but do not attest to flags such as `-race` or focused selection. The archive therefore has incomplete portable invocation provenance for those stronger claims. No existing log has been relabeled or rewritten.

All commands used Go 1.27.3 darwin/arm64. Control source is the `common.patch` tree; candidate source is that tree plus `candidate-only.patch`. Tests ran during preparation before those trees were committed. The initial TDD logs precede addition of the later boundary tests; they must not be treated as final-tree test inventories.

| Retained log | Reported invocation | Source / qualification |
| --- | --- | --- |
| `raw/common-control-tests.log` | `go test ./internal/congestion -count=1` | Control common prerequisite |
| `raw/control-preservation.log` | `go test ./internal/congestion ./internal/ackhandler -count=1` | Control |
| `raw/candidate-preservation.log` | `go test -race ./internal/congestion ./internal/ackhandler -count=1` | Candidate; race flag comes from the session transcript, not the log bytes |
| `raw/candidate-emission.log` | `go test . -run 'Test(BBRPacing\|ConnectionPacketPacing\|Emission)' -count=1` | Candidate; focused selection comes from the session transcript, not the log bytes |
| `raw/sender-boundaries.log` | `go test ./internal/congestion -run TestAssessment964 -v -count=1` | Candidate; verbose test names are retained |
| `raw/tdd-red.log` | `go test ./internal/congestion -run '^TestAssessment964SlowStartDeadline$' -count=1` | Unchanged runtime plus the initial failing test |
| `raw/tdd-green.log` | `go test ./internal/congestion -count=1` | Initial candidate runtime and first regression, before later boundary tests |
| `raw/fixture-tests.log` | `python3 -m unittest discover -s fixture` | Six fixture tests; execution cwd was the assessment directory through an equivalent explicit path |
| `raw/build.log` | `python3 fixture/build.py` | Build script contains the exact two-arm build, endpoint vet and module-tidiness invocations; successful execution was reported by the session, not archived as a command/exit receipt |

The session also reported successful `go vet ./internal/congestion ./internal/ackhandler` and `go mod tidy -diff` in each experiment arm after its preservation tests. Their empty stdout and exact invocation receipts were not archived, so they are reported checks with that provenance limitation. Hosted CI separately validates the unchanged product source on the evidence PR head.
