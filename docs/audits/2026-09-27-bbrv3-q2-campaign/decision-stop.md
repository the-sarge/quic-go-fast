# Q2 Mac decision stop

At 2026-09-27 07:55:16 UTC, case 360 (`core-darwin-arm64-S3-stream-p1-bbrv3`) failed the runner's gateway identity/configuration gate after the 20 S1 observations passed. The runner stopped, preserving the failed verdict and all role receipts. The remaining 49 Mac cases and every cloud lease are unlaunched. This is incomplete evidence, with no adoption or comparative-performance conclusion.

## Concrete cause

The observed gateway source was `e4f322cbbfd4225a4b714e08ec19c958cccadcb0`, and packet version was 2, both as expected. The entire configuration difference is that observed `A` and `B` each include `"Competitor": ""`, while the command generator omitted those fields. The pinned Go router uses string fields without `omitempty`; absent input fields become empty strings in its emitted receipt. The Q2 runner compares raw dictionaries, so it rejected this serialization difference. This is a runner validation defect; the failed verdict has not been rewritten or waived.

Both endpoint roles and the router exited successfully. All reported gateway packet errors, socket drops, noncanonical packets, missing timestamps, send errors, and propagation overflows were zero. The maximum ingress/egress lags were 0.946435/1.168559 ms in the forward direction and 1.494354/1.207342 ms in reverse, below the 5 ms gate. These observations do not qualify the incomplete campaign.

## Cleanup and preservation

The runner recorded successful gateway teardown, exact admission release and post-run clock collection at 07:55:21 UTC. An independent SSH check found no gateway namespaces or campaign router process. The live m4mini admission directory contained no matching `q2-mac-20260927` / `q2-campaign` / `mac-20260927` record. A remaining m4mini `caffeinate -i -t 20280` process, PID 86127 with the campaign's 06:32:16 UTC start time, was terminated and its absence verified. Endpoint fixture and monitoring processes were absent. No unrelated admission or job records were changed.

Raw data remain in the private controller directory `/Users/josh/.local/share/infra/bbr-q635-controller/.local/cloud/bbr-campaign/q2-mac-20260927`. Its additive `decision-stop.json` records SHA-256 hashes of every existing evidence file, the cause and cleanup verification. Private credentials and admission tokens remain private. Original receipts, the runner and prior ledger charges are unchanged.

The 338-minute reservation remains fully charged, including unused time: cumulative experiment reservations are 30.851333333333333 hours (24.718 prior + 0.5 preflight + 338/60 Mac). Cumulative reserved cloud cost remains $30.02; no Q2 cloud resource was launched. The separate 48-hour Mac alias/route cleanup watchdogs remain in place as authorized; their duration is not experiment time.

The `continue-bbrv3-q2-campaign` heartbeat is paused. The operator's explicit failure-stop instruction requires a decision before further comparative launches. A proposed follow-up is a narrowly tested correction of omitted-versus-empty competitor serialization in a new runner revision, with a fresh ledger forecast and explicit disposition of the failed observation and remaining inventory. No correction, reclassification, retry or new reservation has been performed.
