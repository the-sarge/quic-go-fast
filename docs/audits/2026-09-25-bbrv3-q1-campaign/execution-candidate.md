# Campaign execution candidate

This is the frozen campaign-only command recipe backed by
[the native readiness receipt](readiness-receipt.json). Q2 requires separate
dispatch and fresh native resource, clock and lifecycle preflight. The selected
V2 gateway has finite native evidence on Linux, Windows and the declared Mac
path; historical failures remain in the evidence archives.

`lease-plan-candidate.json` owns the proposed seven-lease order. Within each
lease, use its recorded `case_indices`; retain the original controller order,
seed and scenario settings. Each index owns a disjoint eight-port block. A
failed case remains invalid; a retry needs a new recorded port block and budget
reservation, not silent reuse of its original commands.

## Preparation and ownership

The human-authenticated controller on mbp128 owns cloud lifecycle through
infra's `scripts/cloud/bbr-campaign/campaign.py`. For each cloud lease, use the
recorded platform, competitor flag, `gateway_cores: 4`, lifetime and hourly rate.
Carry all prior reservations into the request. Plan/apply only after fresh
quota and price evidence; never change source or request under an active lease.
The absolute expiry starts before provisioning. Reserve the complete lease,
including any failed launch and unused time. Cleanup remains available after
budget exhaustion.

Follow the [clock and event alignment contract](clock-contract.md), retaining
raw offsets and their uncertainty. Record native OS, kernel, CPU topology, Go version, clock status, route,
interface offload state and process/binary hashes. Cloud endpoints expose four
cores with SMT disabled; the gateway candidate exposes four too. macOS uses
the approved scheduling exception: pause available user/sync work and record
remaining OS background activity; efficiency-core placement is not verified.
The Mac test payload follows Thunderbolt to minimax and Ethernet to mbp128;
the home internet is not the payload path. Only test-owned namespaces, aliases
and routes may be changed. Preserve independent management access and a frozen
cleanup deadline.

Stage the pinned native fixture as `/tmp/q1-campaign/q1fixture` on Unix
endpoints or `C:/bbr/q1/q1fixture.exe` on Windows. Stage the pinned Linux router
as `/tmp/q1-campaign/q1router` on the gateway. All modeled commands select `-packet-version 2` with the same pinned gateway binary. Generate one campaign-only mTLS
certificate/key pair using the fixture's `-role cert`, and copy it privately to
both endpoint working directories as `campaign.pem` and `campaign-key.pem`.
Do not commit keys. Record the certificate hash and expiry before a lease;
regenerate rather than reuse an expired preparation certificate.

On the cloud gateway, disable kernel forwarding while the userspace path owns
packets. Disable GRO/LRO/GSO/TSO and `rx-gro-hw` on both measured interfaces and
record the actual feature state. The Mac gateway uses the recorded three
namespaces and offload-disabled veth interfaces; do not change shared physical
NIC offloads or host-wide forwarding. Q1 native archives contain the concrete
setup, staging and cleanup scripts used for those preparation topologies.

## Expand and execute one case

On the controller, choose a warm-up start 25 seconds in the future, then expand
the next index (example below is only command generation):

```sh
python3 case-commands.py 0 --start-unix-ns 1800000000000000000 > case-0-commands.json
```

Use a real current epoch at execution. The expansion contains every role's
exact native command, host, JSON configuration and launch time. It uses the
recorded measurement epoch for router events and the warm-up epoch for the
fixture; completion has no warm-up. For Linux/Windows, send commands through
`gcloud compute ssh HOST --project=bbr-q635-20260925 --zone=us-east1-b
--tunnel-through-iap --command=COMMAND`. For the local Mac path, use `ssh HOST
COMMAND`. Pass the command as one properly quoted argument; do not re-evaluate
JSON with a shell. Windows commands already contain a UTF-16LE encoded
PowerShell script and return result JSON encoded as Base64.

For S1, run the finite `native-path` command to completion before launching
endpoints; it enables kernel forwarding only in the owned gateway scope and
produces a setup receipt, with no userspace router. Join all roles before the
next case. Each modeled case explicitly disables forwarding before starting
its router, preventing the native path from bypassing the model.

For modeled cases, launch the router first. Start each receiver and observe its supplied readiness
command before starting its sender. Keep at least nine independent workers for
L4's router, focal pair and three TCP pairs; the validated preparation driver
uses 12. A worker blocked awaiting one measured role must not prevent another
scheduled role from starting. L4 competitor processes launch 45 seconds before
their declared starts, with flows two and three sharing the same start epoch.
L6–L8 retain their single competitor connection through the 240–270-second bulk
pause. All participant configurations end at the same measurement boundary.

During the first Q2 cloud case, timestamp mandatory orchestration and classify clock/path/resource/export work as per-lease or per-case, and overlapped or serialized. Retain that receipt. Recompute the remaining partition before continuing if mandatory non-overlapped work exceeds its available envelope; never assume the 45-second case overhead covers unmeasured extra work. Clock observations in the clock contract are lease-boundary work.

Check remaining immutable lease time before every case: include the full
future-start, warm-up, measurement, 20-second gateway tail, export and cleanup.
Do not overlap cases on a topology. For Mac, require no active gateway VM workload before launch; record gateway
and endpoint contention throughout, and pause dispatch if that availability
condition is lost. Do not stop unrelated jobs. Keep endpoint/system CPU observations with
the record. The gateway remains alive through the fixture's bounded final
receipt; its tail does not enlarge the measurement denominator.

Collect every role, including failures. Preserve exit status, stderr, raw JSON,
configs and hashes. Validate fixture integrity and receiver-defined useful
bytes, resource observations, controller identity, control probes and completion
status. Validate gateway timestamps, errors, canonical packet domain, loss/CE,
queue limits and both timing maxima. Any nonzero exit, missing receipt or failed
hard gate invalidates the case; the first driver's success does not establish
that its children succeeded. Rate probes additionally require independent
host socket-error counters and probe-specific gateway/receiver packet balance
after draining. Where other traffic shares the focal IP pair, use a filtered
egress observation with zero capture drops and fully drained capture counts;
aggregate all-port gateway counts alone do not identify probe losses.

Finally destroy the cloud lease through the frozen controller request, and
independently verify both VM and disk inventories are empty. For Mac, remove
only the owned namespaces and verify temporary aliases/routes expire. Retain
cleanup failures and stop subsequent leases until ownership is resolved.

## Evidence boundary

The finite expansion check covers all 520 indices: every row retains its
identity; each case has the expected three, five or nine commands (S1 has two
participants plus one completed native-path setup);
and all endpoint schedules share the declared measured end. This checks
command construction, not native execution. The preparation archives retain
real Linux, Windows and Mac command examples and all invalid observations.
Native evidence is indexed in the readiness receipt; source and host identities
remain pinned in the manifest. Command generation alone does not establish
native execution or replace fresh resource/clock observations.
