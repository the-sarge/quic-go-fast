# Q1 campaign preparation

Status: native preparation complete; the [readiness receipt](readiness-receipt.json)
freezes example-level evidence for the declared hosts and command settings.
The product PR owns review and certification before Q1 closes. Q2 requires
separate dispatch and fresh lease/resource/clock preflight. These campaign-only
verification aids ship no controller behavior and retire after Q2.

The operator extended preparation to **16 cumulative hours** on 2026-09-25;
[authorization](https://github.com/the-sarge/quic-go-fast/issues/598#issuecomment-5837538642).
The operator also approved **72 experiment-hours** on 2026-09-25; see the
[approved forecast](budget-proposal.md). The cloud ceiling remains **$100**. Historical
acceptance records retain their original eight-hour allowance. [Infra PR638](https://github.com/the-sarge/infra/pull/638), merged as
`4fcfa6ad7f4cff923fa88d97ce82903b0e734aa9`, enforces the preparation extension.
[Infra PR641](https://github.com/the-sarge/infra/pull/641), merged as
`46ac9a592cb5cd060eabcd6257b6aeb59837b02d`, enforces 72 experiment-hours
and admits the costed four-core gateway.

## Execution boundary

This candidate implements the existing Q1 child and one product PR, against
product source `bfa11d3f698ac98da415c7bcbd9840923180d0bb` and Q1 plan section at
`4161e1b4ca9edf6b743841245ab6e27464401a11`. The native environment, actual
calibration and current budget ledger own readiness; passing local tests or
cross compilation does not establish it. No Q2 comparisons are authorized by
this receipt. Product transport and controller code remain outside this diff.

The supported domain is explicitly configured IPv4 endpoint pairs and the
accepted scenario matrix. QUIC owns wire parsing; the native Linux adapter uses
`golang.org/x/net/ipv4` and mechanically rejects options, fragments, IP packets
over 1460 bytes and protocols other than ICMP/TCP/UDP. It needs campaign-owned
interfaces, disabled offloads and disabled kernel forwarding. The lifecycle
caller owns those reversible changes and restoration. Never run it on shared
production interfaces. Its next-hop MAC is the actual route's next hop (GCE's
virtual router), not an inferred peer MAC.

Representation guarantee: example-level evidence for the frozen native hosts,
not general platform qualification. No persisted product schema, transitional
product authority or maintained-aid exception is introduced. Contract closure
is not triggered for these bounded campaign aids. Initial dispatch context is
the Q1/common contract, named design and source seams, current frontier and
focused prior receipts, inside the accepted 35,000-token input ceiling.
One fresh review and at most one replacement apply to this product PR.

## Components and finite checks

- `fixture`: real native QUIC with the public Reno/BBRv3 selector and direct
  managed packet-I/O lease. Deterministic record identities/content, receiver
  useful-byte/duplicate/integrity accounting, 32-byte control request/response,
  fixed completion through verified EOF and bounded trace/resource observations.
  Unique content excludes the 16-byte fixture header. Stream records must have
  consecutive identities; DATAGRAM messages may reorder, duplicate or disappear.
- `model`: full-IP-byte FIFO serialization followed by independently scheduled
  propagation. Fixed finite queues, deterministic S6 loss, S8 ECT(0)-to-CE and
  Not-ECT early drop, L1 post-service delay, L2 arrival windows and L3 partial
  service/rate transitions and interruption flushing. Pending propagation has
  an explicit rate/delay-derived storage bound; overflow invalidates calibration.
- `router`: Linux AF_PACKET adapter using pinned `gopacket/afpacket` v1.7.1
  ([upstream interface](https://pkg.go.dev/github.com/gopacket/gopacket/afpacket)).
  TPACKET v3 uses an 8 MiB receive ring per direction, 1 ms block retirement
  and bounded 100 ms polling. Ring views are copied before their next read;
  canonical IP parsing remains owned by `x/net/ipv4`. Each direction has one
  model owner and bounded input/output channels. A separate emission worker
  owns each departing frame; it is joined before counters are published. Records kernel socket drops, noncanonical packets, missing kernel
  timestamps, ingress processing delay and scheduled-departure lateness. Any
  socket/send/storage failure, or maximum ingress/egress lateness over 5 ms,
  invalidates its receipt. Native rate/RTT tests must independently establish
  whether that ceiling is adequate for each scenario; it is not a calibration
  claim. CPU demand is outside endpoint allocations.
- `probe`: frozen Linux UDP offered-load source, sink and RTT echo. It counts
  complete IP bytes and received ECN bits, and exposes socket overflow. These
  are path-calibration observations, not QUIC useful delivery.

Focused evidence exercises integrity/duplicate/window accounting, actual QUIC
stream and DATAGRAM transfer, completion, one-pending-request cadence with
warm-up accounting, serialization/byte queues/CE, rate changes without queue
resizing, delay reordering, arrival-window boundaries, interruption flush and
bounded propagation storage. One focused race run covers the concurrent fixture.
Native calibration must additionally prove the packet adapter, offered/received
rates, RTT, actual marks, L3 timing, endpoint allocation and resource limits.
No whole-platform or mutation campaign is implied.

From this directory:

```sh
go test ./fixture ./model
go test -race ./fixture ./model
go vet ./fixture ./model
go mod tidy -diff
GOOS=linux GOARCH=amd64 go vet ./router ./probe
GOOS=linux GOARCH=amd64 go build -o /tmp/q1router ./router
GOOS=linux GOARCH=amd64 go build -o /tmp/q1probe ./probe
GOOS=linux GOARCH=amd64 go build -o /tmp/q1fixture-linux ./fixture
GOOS=windows GOARCH=amd64 go build -o /tmp/q1fixture.exe ./fixture
go build -o /tmp/q1fixture ./fixture
```

Release calibration binaries must also pin `-ldflags '-X main.sourceRevision=SHA'`
for the fixture and record binary hashes. Campaign mTLS keys are created in a
private staging directory and never committed. Both peers receive the same
short-lived campaign trust material; no cloud credentials reach guests.

## Readiness boundary

Linux, Windows and Mac prerequisites have their finite native evidence. The
current manifest selects the same V2 gateway binary on all platforms; historical
receipts retain their original binary identities. L3 uses its first phase as the
fixed queue basis: forward 32,120 bytes and reverse 188,340 bytes.

Readiness is example-level evidence, not a promise that later shared-host runs
will pass. Recheck resources and clock bounds before every lease; retain and
reject every case that fails a hard gate. Mac gateway CPU affinity is not an
exclusive host-wide reservation. Require no active gateway VM workload before
starting a Mac lease and record contention throughout it; pause dispatch if
that availability condition is lost. Do not terminate unrelated jobs.

Resource samples observe cumulative process CPU, heap once per second, full-run
allocations and process peak RSS. Connection setup elapsed includes listener
waiting on the server and dial setup on the sender; neither is included in the
fixed useful-delivery window. Qlog callbacks retain bounded metrics/events but
still incur instrumentation cost. Native CPU saturation must be reported as a
fixture/host limitation where applicable, not interpreted as controller capacity.

## Linux preparation receipt

[The retained preparation receipt](evidence/linux-preparation-summary.json) indexes
102 frozen native configuration/result records, including failures. Four short
Reno/BBRv3 stream/DATAGRAM native smoke cells passed integrity and controller
selection checks; these are correctness observations, not comparisons. Earlier
adapter revisions demonstrated directional rate, marking/drop and queue behavior,
but they do not certify the latest adapter revision.

The unprofiled S3 offered-load probe at `09f9d290` had no socket or send errors,
but maximum admission lateness was 9.062 ms and departure lateness 5.101 ms;
both exceed the 5 ms gate. The earlier L3 phase probe also failed its lateness
gate despite observing the four selected rates. These paths remain invalid.
A separately labeled CPU-profile diagnostic attributed about 27% of sampled CPU
to model advancement and 21% to scheduling. The next candidate reduces heap
packet copying; passing semantic tests is not native calibration.

## Frozen case ordering candidate

`case-inventory.jsonl` enumerates all 460 core and 60 completion cases, with
matched seeds, controller order within each pair, L1/L2 phases and fixed payload
sizes. Recounting the entries gives 129,600 core seconds (36 hours) and at most
3,600 completion measurement seconds. The second completion hour remains
reserved for setup/cleanup and clock checks. `manifest-candidate.json` records
the inventory digest and missing prerequisites; it is not an execution permit.

At launch, add the calibrated absolute `start_unix_ns` to the fixture config.
For a core run that is the beginning of warm-up; set the router's `StartUnixNS`
to fixture start plus warm-up, so L3 and competitor changes remain relative to
the measured interval. Completion has no warm-up and L1 uses phase 2 seconds.
Keep all run IDs, seeds and pair orders from the inventory; do not regenerate a
new ordering between controller runs or delete failed rows.

## TCP competitor mode

Linux `q1fixture -tcp-cubic` accepts a `controller: "cubic"`, `workload: "stream"`
run config with no completion target. It uses the same useful-byte counter,
deterministic payload and one-pending control observer as the QUIC fixture.
The bulk connection binds `-local`; a separate control TCP connection uses the
next port on each endpoint. Both traverse the common modeled bottleneck. This
control connection has independent CUBIC state; its latency is not QUIC stream
multiplexing latency. Records state that distinction explicitly.

The fixture selects kernel CUBIC before SYN exchange and verifies the controller
and negotiated MSS on both connected sockets. MSS is bounded at 1,380 bytes,
keeping full IP packets within the modeled MTU even with a maximum TCP header.
Native smoke on an existing Linux host demonstrated integrity, control replies
and both socket checks; a focused race run passed. That localhost correctness
check is not coexistence calibration or a performance comparison. Optional cloud
competitor hosts, phase orchestration and per-participant resource evidence are
still prerequisites.

TCP useful-delivery accounting is authoritative in the receiver's local JSON;
the sender record owns demand and control observations. The caller must require
both peers to succeed and join their records by the frozen run identity. It must
not interpret the sender's empty receiver counter as zero delivered traffic.
A bounded bulk drain can reach its deadline with queued data still outstanding;
the receiver exports locally without requiring a terminal network receipt on
that expired socket. A native regression retains valid accounting in this case.

## Windows preparation and warm-up regression

[Windows native observations](evidence/windows-preparation-summary.json) cover
all four short Reno/BBRv3 stream/DATAGRAM smoke cells, with integrity, controller
selection, four observed guest cores, resource accounting and actual ECT(0)
send/receive records. The initial shell collection faults were recovered from
the original result files; no comparative conclusions follow from these smokes.

The first native gateway race diagnostic did not complete. Its captured Go stack
identified `model.Queue.finish`: a negative warm-up timestamp overflowed the
subtraction from the no-future-rate-change sentinel, causing an infinite loop.
`TestWarmupServiceBeforeMeasurementEpoch` reproduced the hang and now checks
serialization before measurement for constant and changing-rate schedules.
The sentinel is handled before subtraction in `d0cfa1cb`; normal and race model
checks pass. Native S2 forwarding under the race detector subsequently passed,
including warm-up and shutdown. Lifecycle callers must also bound the adapter
process externally and restore kernel forwarding on failure.

[Modeled Windows path observations](evidence/windows-modeled-path-summary.json)
retain both valid short diagnostics and failed calibration attempts. S3 met the
timing gate at about 72 Mbps observed load; this does not prove 1 Gbps capacity.
A BBRv3 completion observation verified all 16 MiB through EOF in 2.1545688
receiver-local seconds. The full L3 schedule survived interruption and retained
integrity, but failed the 5 ms departure gate at 5.135 ms during warm-up.
A separate SCHED_RR priority-10 hypothesis test failed at 21.199/20.090 ms in the
two directions at the same measured instant. Real-time scheduling did not fix
the limitation and is not a qualified configuration. All failures remain in the
receipts; these observations are not controller comparisons. Windows VMs and
disks were explicitly destroyed and independently verified absent.

`competitor-schedule-candidate.json` pins L4's 0/1/3 demanding TCP streams
and L6–L8's existing-connection bulk absence at measured t=240–270. The
32-byte control probe continues; already admitted data may drain. Actual
pause/resume and per-second receiver counts make that boundary visible. The
measurement denominator remains 300 seconds. Only the accepted 240/270-second
pause on a 60+300-second reliable stream is admitted by the fixture. These
commands still require an independently provisioned and calibrated competitor
pair; schedule generation alone is not native coexistence evidence.

Gateway hardware receive coalescing (`rx-gro-hw`) must be disabled explicitly
as well as software GRO/LRO/GSO/TSO. A native CUBIC diagnostic exposed 2,788-byte
coalesced TCP packets with software GRO already off; the adapter rejected them
under its 1,460-byte canonical contract. Capture and retain the actual feature
state of both gateway interfaces. The canonical contract is unchanged.

Each frozen case receives a disjoint eight-port block derived from its inventory
index, as specified in the competitor schedule. Reusing a fixed TCP client port
immediately after a run can fail in `TIME_WAIT`; that setup failure is retained
and invalidates the case. A retry needs its own recorded unused block.

The gateway candidate at `d17e45fe` includes socket-submission time in its
departure-lateness counter. A short S3 offered-load diagnostic with `GOGC=400`
and `GOMEMLIMIT=4GiB` passed at 3.810 ms admission / 3.281 ms submission lateness
and about 240 MiB peak RSS. This is a bounded runtime-setting candidate, not
long-duration qualification. A prior GC-suppressed hypothesis run also passed,
but used about 2.3 GiB in ten seconds and is not the selected configuration.

## Isolated Linux competitor and sustained-rate receipt

[The native receipt](evidence/linux-competitor-summary.json) indexes the retained
records from the five-VM launch. Its first attempt exceeded the project's global
32-vCPU quota despite passing regional checks. GCP approved the required 36;
[infra PR640](https://github.com/the-sarge/infra/pull/640) adds that missing
preflight. Both the failed and successful launches retained their reservations.

With hardware receive coalescing disabled and the bounded GC setting above,
the full 60+300-second L7 CUBIC coexistence check passed every endpoint and
gateway gate. Maximum gateway delays stayed below 3.2 ms. All 300 measured
control probes completed; the sender observed its bulk pause at 240.124 seconds
and resumption at 270.000 seconds. Queued bytes drained, then delivery was zero
through most of the 30-second absence. This proves the recorded orchestration
example, not a paired controller comparison. The later TCP local-export revision
also passed a native cloud smoke with both peers' records collected successfully.

A 210-second S3 offered-load check delivered essentially 1,000 Mbps after the
first second, with 4.958 ms maximum admission delay and 4.064 ms maximum socket
submission lateness. Peak gateway RSS was about 245 MiB. The passing admission
margin is only 0.042 ms; future violations still invalidate a run. The receiver
recorded 9,891 reordered UDP packets, with no invalid payloads or socket drops.
This native substrate observation must accompany claims about the modeled FIFO.

The archive includes invalid timing attempts, the hardware-coalescing packet
capture, the port-reuse setup failure and the idle gateway readiness-check
failure. The intended L4 0/1/3-flow run never launched after a staging timeout;
it is unmeasured. All 20 managed resources were explicitly destroyed, and
independent instance/disk inventories were empty before the immutable expiry.
Final-source calibration for the remaining directions/scenarios, Windows with
the selected gateway settings, Mac prerequisites, and the complete costed
command manifest remain open. Q1 is not complete.

## Approved Mac scheduling exception

On 2026-09-25 the operator accepted otherwise-idle native Mac endpoint hosts
with `GOMAXPROCS=4`, identical Reno/BBRv3 settings, separate gateway resources
and recorded CPU contention. These results must not be described as using four
isolated physical cores. Linux and Windows resource requirements are unchanged.

The operator connected M4 mini to minimax. Both hosts detect their peer and
report an 80 Gb/s Thunderbolt/USB4 link. This is connection evidence only;
native IP path, rate, timing, clock and gateway calibration remain necessary.
The S3 1,000 Mb/s capacity is a local emulated-path setting; the home internet
uplink carries none of the test payload. The existing mini–MacBook Pro link is
also still present, so route evidence must prove test traffic uses the gateway.

## Native Mac probe and teardown bound

The UDP probe also builds natively for Darwin. Darwin returns traffic class as
`IP_RECVTOS`; it has no Linux `SO_RXQ_OVFL` ancillary counter. A zero `SocketDrops`
field on Mac therefore proves nothing: collect host-wide UDP full-buffer-drop
counters before and after each probe and require zero growth for admission.
The receipt labels the counter capability. Darwin probes request a 6 MiB
per-socket receive buffer and record the actual value. The earlier 4 MiB
reverse S3 observation lost 3,977 packets to the Mac mini socket buffer and
remains invalid; enlarging the probe buffer does not change host-wide settings
or the QUIC fixture. See [Apple IP socket documentation](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/man/man4/ip.4).

The gateway remains alive for 20 seconds after the measured window, covering
the fixture’s 15-second bounded final receipt exchange plus five seconds of
shutdown margin. This does not enlarge the measurement denominator. Charge
the tail as command/teardown overhead. The earlier three-second tail could cut
off receipt exchange on L4’s one-second-RTT path; retain that invalid observation.

Windows has a plain IPv4 rate probe with an 8 MiB requested socket buffer. It
records that per-socket drop and ECN metadata are unavailable in this probe;
require unchanged host UDP Receive Errors around a run. It rejects `-ect` and
unexpected ancillary data. The native QUIC fixture and gateway observations
still own Windows ECN evidence. This is a rate/RTT check, not a new Windows
metadata parser. Unix ancillary handling remains delegated to `x/sys/unix`.

For native path capacity checks, two independent 600 Mb/s probe streams may
share the same modeled FIFO with two Go processors per process (`-processors 2`).
They retain separate sequence counters; aggregate full-IP byte rates and require
zero host receive-buffer-error growth and exact gateway/receiver packet balance
after the drain. This tests path capacity without requiring one plain UDP
receive loop to absorb the entire gigabit rate. It does not add campaign
workloads or change the focal fixture’s four-processor resource contract.

## Later Linux and native Mac preparation receipts

[Linux final receipts](evidence/linux-final-summary.json) retain the remaining
two-core gateway checks and failures. Both full L3 directional schedules and
the recorded impairment checks passed. Reverse S3, L6 and L8 exceeded the
unchanged 5 ms ingress gate (5.609, 11.474 and 6.024 ms respectively). They are
invalid. L4 also failed: the old three-second router tail cut off focal receipt
exchange, and an eight-worker driver could not run all nine roles. The bounded
retry uses the 20-second tail, 12 driver workers, 45-second CUBIC launch lead,
and checks every role's result. All cloud resources from this lease were
explicitly destroyed; independent VM and disk inventories were empty.

[Mac receipts](evidence/mac-native-summary.json) retain native S3 correctness
for Reno/BBRv3 stream and DATAGRAM, a full L3 Reno fixture, completion, and
bidirectional rate diagnostics. Gateway timing passed the recorded Mac cases.
Forward S3 sustained the modeled gigabit rate. Reverse S3 is **invalid**:
the 4 MiB socket lost 3,977 packets, and the 6 MiB socket lost 5,695. The
prepared two-stream diagnostic has not yet run. Recorded whole-host CPU load
also prevents claiming otherwise-idle comparative readiness from these checks.
The temporary addresses/routes expired automatically; owned gateway namespaces
were removed. No home-internet payload path or Mac physical-core-isolation
claim is made.

The raw archives include failed observations and per-case configurations.
Private keys, cloud billing identity and personal host inventories remain
outside committed evidence. These are preparation receipts, not performance
comparisons or a Q1 readiness declaration. The current finite allocation and
conditional competitor lease partition are in [the budget forecast](budget-proposal.md).

## Packet-ring diagnosis and candidate

The four-core cloud profile still exceeded the 5 ms timing gate with the V3
packet ring. L6's maximum kernel-to-reader delay was 5.768 ms. Reducing Go
processors from four to one made a short S3 diagnostic substantially worse
(29.172 ms ingress); that configuration is rejected.

A separate V2 ring diagnostic retained four Go processors and the unchanged
packet model, GOGC400 and memory limit. Both 210-second S3 directions passed:
forward maxima 3.959 ms ingress/3.243 ms egress, reverse 3.577/3.925 ms, roughly
1,000 Mb/s, no ring/socket drops or invalid payloads. Native UDP reordering
remains recorded. This supports a packet-ring change as a candidate; it does
not qualify the remaining scenarios or Windows path.

The router now accepts `-packet-version 2` or `3`, with default 3 preserving
previous Mac commands. The cloud command candidate explicitly selects 2.
The receipt records ring version, source and Go version. `gopacket` owns both
ring representations and socket statistics; V2 populates its first counter
set, V3 its second. Sum the returned drop counters so neither ABI silently
loses that gate. No handwritten kernel header parser is introduced.

[Linux's packet-mmap documentation](https://docs.kernel.org/networking/packet_mmap.html)
describes V3 block-level polling/timeout versus V2 packet-level polling. The
native observations support testing that distinction; they do not prove that
all observed delays came from block retirement. Final-source validation of
L3, L4 and L6–L8, a short S8 marking check, and the selected Windows path remain
required before adoption of this candidate. The operator approved a 72-hour experiment ceiling, and the final-source
45-minute Linux validation was reserved before launch. L6 passed all five
roles on that selected build; the remaining scheduled cases are still running.
The [source graph](source-graph-candidate.json) records binary hashes, compiler
versions and component revisions, including the legacy Linux calibration
probe's incomplete source provenance. It is not a Q2 participant.

## Final selected cloud gateway: Linux receipt

[The final Linux receipt](evidence/linux-v2-final-summary.json) retains all eight
scheduled checks. L6, L8 and L7 passed every process and the unchanged 5 ms
gateway gate. L4 passed all nine native commands expanded directly from the
frozen inventory. The 300-second L3 rate probe observed the configured
5/20/1/20 Mb/s steady phases and passed timing.

The final pinned probe passed S8 ECT, S8 Not-ECT and short S3. S8's 85,775
marked packets exactly matched the receiver's CE count; Not-ECT produced
42,524 early drops and no marks. S3 observed 1,000.004 Mb/s. All three had
exact delivered/received packet balance, no socket drops and no invalid or
duplicate payloads. The probe transition is explicit: L3 retained the running
legacy binary, verified through its process executable hash; only subsequent
short checks used the pinned46723c74 probe. No older result is relabeled.

The selected router is da6655e0, Go1.27.1, TPACKET_V2, GOGC400 and a4GiB
Go memory limit on the four-core cloud gateway. All20 resources were destroyed
before immutable expiry; independent VM/disk inventories were empty. Windows
setup with this build failed when the gateway file transfer timed out, before
any measurements; cleanup left zero VMs/disks and the reservation remains
charged. A bounded retry uses the existing SSH transport and a compressed,
hash-verified bundle. Mac idle-window validation, final manifest certification
and review remain open; Q2 remains blocked.


## Second Mac window: reverse capacity remains invalid

[The second Mac receipt](evidence/mac-followup-summary.json) retains the
two-flow, 210-second reverse S3 diagnostic. It reached 999.922 Mb/s but did
not pass: the gateway delivered 17,996,379 packets and the two receivers
counted 17,995,020. The Mac mini's host-wide UDP full-buffer-drop counter
increased by 179. No invalid or duplicate payloads were observed. The gateway
passed its timing gate (maximum ingress 1.401 ms, submission lateness 1.357 ms)
and reported no packet-socket drops or send errors. The remaining 1,180 missing
packets are not explained by the recorded host-wide counter; do not attribute
all loss to a socket buffer or to background activity.

Both hosts had background activity before measurement. During the diagnostic,
mean CPU idle was 59.75% on M4 mini and 66.52% on MacBook Pro, including the
probe workload. These records establish neither otherwise-idle scheduling nor
reverse-path calibration. A quiet-host check with observations of interface
and socket losses remains necessary before declaring Mac readiness. No Q2
comparison was run, and no rate or scheduling requirement was relaxed.

The gateway namespaces were removed, and both Mac watchdogs removed their
aliases, host routes and root state. Independent cleanup observations are
retained in the archive. [The failed Windows setup receipt](evidence/windows-staging-failure-summary.json)
retains the earlier staging timeout and independent empty resource inventories.


## Latest Windows and Mac follow-up

[Windows retry evidence](evidence/windows-final-retry-summary.json) confirms that
compressed, hash-verified gateway staging works. Native generated L3 commands
and finite BBR completion passed. The paced UDP probes offered only about
68 Mb/s. Unpaced probes reached 1,000.054 and 999.470 Mb/s with exact packet
balance, but both failed the unchanged 5 ms gateway timing gate (maximum
ingress about 5.734 and 5.735 ms). These are not successful S3 qualifications.
A two-second unpaced sender diagnostic ran while gateway forwarding was off;
it received no packets and supports only a sender-rate observation. Original
parser and clock-observer failures are preserved separately from successful
re-evaluations and reruns. All cloud VMs and disks were removed and independently
verified absent.

The calibration probe now permits an explicit bounded pacing batch of at most
256 packets, preserving the default of one. This is a candidate correction to
the Windows offered-load limitation, not a demonstrated OS diagnosis. It needs
native rate, loss and timing validation on its recorded binary/source identity.

[The latest Mac receipt](evidence/mac-counter-summary.json) reached 998.908 Mb/s,
passed gateway timing and showed no growth in either host's UDP full-buffer-drop
counter. Its apparent 928-packet difference includes small other packets: the
gateway's IP-byte total exceeds received probe bytes by only 101,627 bytes,
rather than 928 times 1,460. Aggregate counters cannot establish probe loss.
A filtered egress capture is prepared to count only the two probe destination
ports, requiring zero capture drops and exact receiver balance. The original
failed aggregate evaluation remains preserved. Interface records are redacted
for hardware and non-campaign addresses; original hashes remain in the archive.

The operator clarified that remaining macOS media analysis/indexing is acceptable
after pausing available user/sync workloads. Record CPU contention; neither
physical-core isolation nor efficiency-core placement is verified. This updates
the earlier otherwise-idle prerequisite and does not change packet-loss or
5 ms timing gates. Both root watchdogs cleaned up their temporary network settings,
and the gateway namespaces were independently verified absent. Q2 remains blocked.

## Windows capacity passes; longer Mac setup window

[The Windows pacing receipt](evidence/windows-batch-summary.json) retains both
initial and lower-pressure checks. The explicit 256-packet batch achieved the
requested offered rate, but sustained forward timing still failed at 5.171 ms.
The bounded follow-up used 128-packet batches with requested 1.05 Gb/s. Both
210-second directions passed: 999.725 and 1,000.058 Mb/s, exact gateway/receiver
balance (17,992,356 and 17,997,669 packets), no UDP error growth and no invalid
or duplicate payloads. Maximum ingress/submission delays were 3.575/3.247 ms
forward and 3.202/3.081 ms reverse. The gateway source, model and 5 ms gate did
not change. All 18 cloud resources were removed; independent VM/disk inventories
were empty. The temporary validation coordinator hold had an independent finite
resumption watchdog; the original cleanup resumed and both processes exited.

[The longer Mac window](evidence/mac-eight-hour-summary.json) follows the
operator's request to avoid repeated administrator setup. Each root watchdog
now removes its temporary alias and host route after eight hours (11:50:37
and 11:50:54 UTC on 2026-09-26). The full eight hours, including unused time,
remain reserved. This is network availability, not eight hours of extra cases
or a requirement to leave the hosts idle. Shared/default routes are unchanged;
only the agent-owned gateway namespaces are recreated for finite checks.

Filtered capture proved that mixed background traffic was not the whole loss
explanation. The first filtered reverse run captured 17,996,278 probe packets
without capture loss and received 17,993,206: 3,072 packets missing, including
135 additional host-wide UDP full-buffer drops. Its gateway ingress delay also
failed at 5.044 ms. Lowering total offered traffic from 1.2 to 1.05 Gb/s passed
gateway timing but still lost 6,010 probe packets, with 400 full-buffer drops.
The mini's network-memory denial counter increased from 8,590 to 19,816;
MBP's remained unchanged. These counts suggest receiver-side allocation
pressure, but do not map one-to-one to packet losses or prove general RAM
exhaustion. They do not justify changing global kernel settings. Original
invalid outcomes, precise filtered counts and CPU observations remain retained.

## Mac burst diagnosis

The same two-flow, 210-second reverse probe passed through kernel forwarding
in the owned test namespace: 18,860,806 packets sent, filtered-captured and
received, 1,049.004 Mb/s, no capture loss, no UDP full-buffer growth and no
network-memory denial growth. This is a physical-path baseline, not modeled
S3 qualification. It makes emulator output bursts a stronger explanation than
a general inability of the Mac or local route to sustain gigabit traffic.

The diagnostic router at `5331ae1f` optionally spaces socket submissions above
the modeled rate. Neither the model nor the 5 ms gate changes. At 1.05 and
1.2 Gb/s submission ceilings, receiver/capture counts matched without host
error growth, but gateway timing failed (maximum ingress 94.998 and 285.329 ms;
egress 6.871 and 28.214 ms). A separately labeled CPU profile attributed
87.72% of sampled CPU to the emission worker, including its clock-based wait.
The profile is not qualification evidence. At a 2 Gb/s submission ceiling,
gateway timing passed but 1,236 captured probe packets were missing at the
receiver, with 933 additional UDP full-buffer drops. None of these pacing
settings qualifies the Mac path. The experiment-only spacing option was reverted; its exact source commit
and failed observations remain retained.

The exact cloud-qualified V2 binary without added spacing eliminated measured
receiver loss in both directions. Reverse passed all gates at 1,000.045 Mb/s:
17,996,252 filtered-captured and received packets, no UDP or capture drops,
maximum ingress/submission 4.333/4.114 ms. Forward also balanced exactly at
17,995,331 packets and 999.999 Mb/s without UDP or capture drops, but failed
its ingress gate at 6.532 ms (submission 4.628 ms). These results support the
receive-batching hypothesis for the previous losses; they do not qualify the
forward path. Concurrent VM activity was observed on minimax, without proving
that it caused the maximum delay. The next bounded check changes only the
owned gateway process's scheduling priority to nice -20; unrelated work and
global scheduler settings are untouched. All raw records remain retained.

## Selected Mac V2 path and command receipt

The priority trial still failed admission timing at 5.514 ms, despite exact
17,996,463-packet balance and no host UDP error growth. It was not selected.
After the observed VM workload ended, the final unprioritized forward check
passed at 1,000.000 Mb/s, with exactly 17,996,392 filtered-captured and received
packets, no UDP or capture drops, and maximum ingress/submission 1.168/1.148 ms.
The per-second gateway observations contain no QEMU process. Together with the
earlier unprioritized V2 reverse pass, this qualifies the declared capacity
example. It supports a contention explanation for the timing failures without
proving which scheduler or runtime event caused each maximum.

The generated native Mac BBR completion case verified all 16,777,216 useful
bytes without corruption or duplicates. Every role and before/after clock
check passed. Its native S1 setup command enabled kernel forwarding, and the
following modeled command reset it to zero before starting the selected V2
gateway. All 520 command expansions passed identity, participant-count,
forwarding-mode and finite endpoint-deadline checks. The 70 S1 cases contain
one completed native-path setup plus two endpoint commands; the other 450
use the modeled router. Earlier full Mac L3 and stream/DATAGRAM correctness
evidence remains attributed to its original V3 source, while final Linux and
Windows L3/impairment evidence covers the shared model and selected V2 binary.
These are Q1 checks, not Q2 controller comparisons.

Native preparation reached this receipt at 15.976 observed hours, before its
16-hour stop. Required PR review and certification remain distinct from native
readiness; no further preparation experiment is implied. All cloud resources
are removed and owned gateway namespaces are absent. Mac addresses and routes
remain under their frozen eight-hour watchdogs; their expiry is not yet a
completed cleanup observation.
