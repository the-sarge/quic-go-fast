# Q2 first cloud preflight stop

Attempt `q2-linux-20260929-lease01` launched at 2026-09-29T10:09:49.233959Z with an eight-hour reservation and immutable expiry at 18:09:51.371745Z. Infrastructure apply succeeded. The native preflight then stopped at approximately 10:12:29 UTC, before any comparison case or boundary clock observation completed, because `gateway-native-path` returned exit 1.

## Cause and evidence boundary

The new campaign aid used `set -e` around two pings to GCE's virtual gateway addresses, followed by neighbor collection. Both interface offload reports and route/address records were emitted, then the command stopped at a ping. [Google documents that virtual subnet gateways do not answer ICMP](https://docs.cloud.google.com/vpc/docs/subnets). The agent incorrectly promoted an ARP-warming ping from the historical recipe into a required reachability gate. This is a preflight-aid error, not evidence of a fixture, BBR, native endpoint path or impairment-model failure.

The valid requirement is to establish the expected route and virtual-router neighbor identities, then observe the prescribed endpoint-to-endpoint positive and forwarding-disabled negative paths. Any later candidate must retain those checks and the original cases, seeds, controller pairings and order. Do not edit or retry this failed attempt. Its runner is frozen at SHA-256 `1b95735f669484c6c7028a14338e94236346fee30582aafe128d91c077e3a456`, committed at `82b5a9b37e8524f48985f30023c7d8b4fbb7ab4c`.

No cloud comparison case was created. Mandatory cleanup completed and independent VM/disk inventories were empty at 10:18:30 UTC. The lifecycle is marked destroyed and the heartbeat is paused. The driver's 210-second destroy timeout left a cleanup error and an owned stale state lock. After proving no active controller or OpenTofu process remained, the exact interrupted-cleanup lock was released and the same frozen request completed cleanup. Preserve the failed lease result; the separate cleanup receipt does not relabel it. The successful 70-case Mac evidence remains unchanged and quiet-host duties remain released. Q2 stays incomplete; subsequent cloud launches require the operator's decision.

## Accounting

Keep the failed cloud attempt's full eight-hour / $10.65 reservation, even though teardown occurred early. Cumulative reservations are 44.984667 experiment-hours and $40.67. The existing forecast assumed this lease would complete its 108 cases; an additional full eight-hour corrected attempt would raise that forecast to 84.718 hours and $106.863333, below the unchanged $150 / 96-hour ceilings, subject to fresh prices and later lease timing checks. No retry reservation is made by this forecast. Preparation remains separately accounted under 72 hours, excluding AFK idle.

Private `independent-cleanup-verification.json` and `final-launch-snapshot.json` establish cleanup; `failed-evidence-sha256.json` freezes 104 files. Current preparation through 10:18:32 UTC is conservatively rounded to 28 active minutes, bringing the separate estimate to 22.631657 hours before final closeout bookkeeping. No AFK gaps or full native experiment runtime were charged as preparation.
