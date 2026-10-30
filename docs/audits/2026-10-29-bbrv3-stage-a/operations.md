# Prepared operations; not executed

This is a finite operator recipe, not an autonomous runner. All mutation, calibration and observation commands below require the future resource/launch decision. Use the immutable registration/build hashes and a fresh private `/var/tmp/q2-stage-a` directory on each owned guest. `PROJECT`, `ZONE`, `REQUEST`, `LEASE_DIR` and start epochs must be recorded in a separately hashed launch binding after access/resource approval; they are unresolved now, not guessed credentials. Do not call the controller's `validate` as a read-only probe: its CLI creates/locks evidence state even for that action.

## Read-only refresh before allocation

```sh
gcloud projects describe "$PROJECT" --format='json(projectId,lifecycleState)'
gcloud compute regions describe "${ZONE%-*}" --project="$PROJECT" --format=json
gcloud compute project-info describe --project="$PROJECT" --format=json
gcloud compute instances list --project="$PROJECT" --format=json
gcloud compute disks list --project="$PROJECT" --format=json
gcloud compute addresses list --project="$PROJECT" --format=json
gcloud compute snapshots list --project="$PROJECT" --format=json
gcloud compute forwarding-rules list --project="$PROJECT" --format=json
gcloud storage buckets list --project="$PROJECT" --format=json
```

Retain command start/end/exit/stderr. An inaccessible API is a failed gate, never an empty-resource result. Confirm billing-account linkage and scoped IAM with the authorized account without exporting credential material. Reconcile settled billing/export, storage/state/egress and all retained reservations/effort. Obtain a less-than-24-hour all-inclusive quote for the bound image, zone and three-VM topology. Freeze complete inputs for the controller with `platform=linux`, `competitors=false`, `gateway_cores=4`, `lifetime_minutes=90`, current reconciled hours/dollars/effort, and price timestamp. The request must fail if any required evidence or numeric balance is absent. The current guard patch is an unapplied proposal; its eventual separate infra tests and effective-source hash must accompany the request.

## Setup and lease deadline

After separate infrastructure approval, use the existing lifecycle controller's `doctor`, `init`, `plan` and `apply` actions with `--config "$REQUEST"`; do not create a replacement controller. `plan` also writes state and establishes expiry, so record the first orchestration timestamp and account for planning time. Explicitly verify expiration leaves enough of the 90-minute lease for all remaining phases; never extend the deadline to recover setup time. The existing infrastructure uses per-instance deletion deadlines. Retain the actual provider scheduling receipt and controller trap/cleanup ownership. Prepare the independent cleanup invocation before apply. Stage only the registered candidate/reference fixture and probe binaries; verify `sha256sum` and `go version -m` on the actual endpoints. Generate private mTLS material once with `./candidate-fixture -role cert`, securely stage it on both endpoints, and keep it out of Git and exported public results.

Run and retain these commands on each participant before calibration and again at cleanup (through bounded `gcloud compute ssh HOST --project="$PROJECT" --zone="$ZONE" --tunnel-through-iap --command=...`):

```sh
uname -a
lscpu
free -b
cat /proc/self/status
cat /proc/stat
cat /proc/loadavg
ps -eo pid,comm,pcpu,pmem --sort=-pcpu
ip -details address show
ip route show table all
tc -s qdisc show
chronyc tracking
chronyc sources -v
go version
python3 --version
ethtool --version
```

Capture cgroup CPU/memory limits, actual visible cores and steal/utilization over the check interval. Stop for other workloads, contention or insufficient endpoint/gateway allocation. `GOMAXPROCS=4` is not physical isolation. On each gateway interface (`ens4`, `ens5`, verified against the instance binding), record `ethtool -k`, `ip -s link` and driver/kernel facts. After recording originals, the owned setup disables GRO/GSO/TSO/LRO as supported with `sudo ethtool -K INTERFACE gro off gso off tso off lro off`; reread settings and fail if required offloads remain enabled. Set `sudo sysctl -w net.ipv4.ip_forward=1` for S1 only. Verify routes traverse the independent gateway, no emulator process is running, MTU is 1460 and both directions work. This setup changes only the future exclusive lease, never a shared local host.

## Clock observations

`python3 clock-observe.py "$PROJECT" "$ZONE" "$LEASE_DIR/clocks-before.json"` and, at cleanup, the same command with `clocks-after.json` use the copied finite observer, with [source/patch identity](clock-source.json). It sends ten management timestamp requests per host, with bounded reads and no clock adjustment. Retain partial stderr and phase failure if it aborts. Capture chrony status at both boundaries and during any suspected source/step change. The inherited [clock contract](../2026-09-25-bbrv3-q1-campaign/clock-contract.md) governs interval intersections, source assumptions, wall-clock control RTTs and event translations. No clock observer was run during this preparation. Empty/inconsistent intersections, clock steps or missing participants fail alignment. Raw host timestamps do not become synchronized merely by sharing the numeric epoch; disclose uncertainty and use receiver-local useful delivery for claims that need no cross-host alignment.

## Bounded S1 calibration within 15 minutes

Use the registered probe binary and retain native tool/hash/CPU/clock receipts. No modeled L1/L6, GSO or marked-ECN calibration is included. For each command pair set one start epoch 25 seconds in the future, launch the receiver/echo first, and retain both exits and JSON. All commands run in the private staging directory with `GOMAXPROCS=4`; apply `timeout --signal=TERM --kill-after=2 65` outside each command. These are prepared experimental commands and have not run:

```sh
./probe -role receive -local 10.63.2.10:25000 -start "$CAL_START" -seconds 30 -output forward-receive.json
./probe -role send -local 10.63.1.10:25000 -peer 10.63.2.10:25000 -start "$CAL_START" -seconds 30 -rate 0 -output forward-send.json
./probe -role receive -local 10.63.1.10:25001 -start "$CAL_START" -seconds 30 -output reverse-receive.json
./probe -role send -local 10.63.2.10:25001 -peer 10.63.1.10:25001 -start "$CAL_START" -seconds 30 -rate 0 -output reverse-send.json
./probe -role echo -local 10.63.2.10:25002 -start "$CAL_START" -seconds 30 -output rtt-echo.json
./probe -role ping -local 10.63.1.10:25002 -peer 10.63.2.10:25002 -start "$CAL_START" -seconds 30 -output rtt-ping.json
```

Assign a fresh `CAL_START` to each pair; never run all six commands simultaneously. Preserve offered/received IP-byte rates, drops, errors, RTT samples, interface counters and CPU use. Verify receiver identity, direction, route/gateway counters and finite nonzero delivery/RTT observations, no socket failures and no accidental shaping. Compare the observed S1 capacity/RTT and loss with the inherited accepted native-path calibration criteria before permitting comparisons; if missing, inconsistent or unexpectedly limited, stop for calibration disposition rather than inventing a pass threshold. These probes do not measure QUIC useful throughput. No new model calibration is needed for S1 native routing, and none is claimed for later modeled stages.

## Six observations and stop rules

Use `register.py` to print each fixed observation's two-role commands and configs, binding the actual epoch and preserving hashes. Create config files exclusively and require fresh stderr/stdout/result paths before launch. Collect receiver readiness, launch sender, retain all output and role exit codes, then allow the complete 255-second envelope before the next observation. No concurrent focal observations, competitor or Stage B command is admitted. Require actual controller and both source identities, nonzero useful payload, integrity/duplicate accounting, complete control/resource/trace records and at least one accepted multi-packet callback for each claimed candidate controller/workload. The receipt must retain both endpoints' counters. Missing or ambiguous engagement leaves that claim incomplete; do not change demand or repeat until it appears.

Stop on any failed required gate, corruption, incomplete role/exit/evidence, lost isolation, clock discontinuity, deadline/budget risk or substantial unexplained advisory regression. Retain the failed attempt and full reservation. Review absolute useful throughput, CPU/GiB, peak memory and control p95 for C-Reno/R-Reno and C-BBR/C-Reno. Do not compute a ratio with missing/zero denominator or label one repetition statistically accepted.

## Export and cleanup within 15 minutes

Export raw configs, role outputs/stderr/exits, resource/clock samples, timing phases, native capability/offload facts, controller/instance identities and accounting to the private lease archive; hash every file and verify a second copy before deletion. Record export start/end and billed/retained storage/egress. Collect final clocks/contention/counters before teardown. Then invoke the existing controller with the frozen request:

```sh
python3 /Users/josh/.local/share/infra/bbr-q635-controller/scripts/cloud/bbr-campaign/campaign.py destroy --config "$REQUEST"
```

Independently rerun the inventory commands above. Its built-in cleanup checks instances/disks only; additionally inspect addresses, snapshots, forwarding rules, network resources and state bucket retention. Archive state/billing records, identify anything retained and its owner/charge; do not delete unrelated resources or assume auto-delete covers storage/egress. Cleanup failure is a required-action state, not completion. Finish the lease before waiting for an operator decision. A future results packet must compare reserved versus actual timing/cost, identify overlaps/failures, retain historical flags, and use applicable observed durations for any newly authorized Stage B request. Linux timing cannot certify other platforms or topologies.
