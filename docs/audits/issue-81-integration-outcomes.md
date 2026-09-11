# Integration outcome assertion repairs

[Issue #81](https://github.com/the-sarge/quic-go-fast/issues/81) repairs four observations in existing integration fixtures. Production code, timeout durations, the Linux multiplex skip, and historical investigations remain unchanged. The context-cause typo is in `TestServerTransportClose`, although the issue describes it as a configuration-error test.

Both multiplex listeners now serve the existing payload and both dial workers report their errors. Migration snapshots client-to-server old-path packets before switching and compares after the receiver observes EOF; server-to-client ACK traffic is explicitly excluded. The caller-owned socket test sends from the original socket after closing `Listen`, with an observer socket verifying payload and source address. Transport closure checks `conn2`'s cause after awaiting `conn2`.

## Negative controls

These temporary fixture mutations were each run once on macOS arm64 / Go 1.27.0 / QUIC v1, then removed. Each failed at the repaired assertion rather than merely timing out at the package level.

| Missing outcome | Observed failure |
| --- | --- |
| Omit server 1's serving helper | Server 1 receive result reports `error accepting stream: context deadline exceeded`. |
| Omit server 2's serving helper | Server 2 receive result reports `error accepting stream: context deadline exceeded`. |
| Omit the migration `path.Switch()` | Old-path client count grows from 834 to 1219 across the completed transfer. |
| Close the original caller-owned socket before its lifetime observation | Writing that socket reports `use of closed network connection`. |
| Close `conn2` with application error 42 before transport closure | The second cause assertion reports application error 42 instead of idle timeout; `conn1`'s preceding idle-timeout assertion passes. |

These are example-level regression controls at the issue's existing seams, not an exhaustive protocol guarantee or a maintained mutation harness. Positive checks and exact-head review/CI receipts are recorded in the PR; review allowance is one initial RAS review and at most one replacement following accepted fixes.

The first repaired socket check attempted a raw read on the original socket. A focused race run exposed a competing-reader timeout because the transport can still own reads while retained connection entries drain. The final check receives on an observer socket while requiring a successful send from the original socket; it does not change transport teardown or extend deadlines.
