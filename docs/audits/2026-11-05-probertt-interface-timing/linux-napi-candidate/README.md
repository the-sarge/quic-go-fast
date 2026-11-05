# Inert Linux IRQ/NAPI candidate for issue #1210

This candidate was compiled on minimax with `bpftrace --mode codegen --verify-llvm-ir`; it has not been loaded, attached, or used to generate traffic. The active diagnostic overlay files remain unchanged. Use this only if the Mac BPF result leaves a question about Linux receive service, under the existing host lease/operator workflow. This is an observation, not a controller or NIC configuration change.

## Verified host facts

- Host kernel: `7.0.0-30-generic`, Ubuntu build `#30`, x86_64, PREEMPT_DYNAMIC.
- Interface: `enp97s0`, PCI `0000:61:00.0`, driver `r8169`, firmware `rtl8127a-1_0.0.5 05/14/25`.
- NIC IRQ: 66, effective CPU 2 at inventory time; requested affinity `0-31`. Recheck the IRQ before a later capture; the script intentionally pins the observed interface and IRQ.
- Module: `/lib/modules/7.0.0-30-generic/kernel/drivers/net/ethernet/realtek/r8169.ko.zst`; SHA-256 `bffaf8f83fcde076ca16a652e106f915f72fb32eafd3abb80d9328a6150bc534`; srcversion `997888E1ECA4EA2C2C90B45`; vermagic `7.0.0-30-generic SMP preempt mod_unload modversions`.
- Package: `linux-modules-7.0.0-30-generic` version `7.0.0-30.30`, source package `linux` version `7.0.0-30.30`. Installed headers match; full kernel source is not installed.
- Tools: bpftrace `0.25.0-1ubuntu1` (`v0.25.0`), bpfcc-tools `0.35.0+ds-1ubuntu2`, Linux tools package `7.0.0-30.30` (`perf version 7.0.14`), bpftool `v7.7.0` with libbpf `v1.7`, trace-cmd `3.3.3`.
- Kernel and module BTF are available at `/sys/kernel/btf/vmlinux` and `/sys/kernel/btf/r8169`. BPF events, kprobes, fprobes, function tracing and BTF module support are enabled.
- Typed fentry/fexit signatures are available for `r8169:rtl8169_interrupt`, `r8169:rtl8169_poll`, `r8169:rtl_rx`, and `r8169:rtl8169_start_xmit`.
- Available tracepoints include `net:napi_gro_receive_entry`, `net:netif_receive_skb_entry`, `net:net_dev_start_xmit`, `net:net_dev_xmit`, `napi:napi_poll`, and IRQ/softirq entry/exit events.

## Selection and interpretation

`observe.bt` accepts the local UDP port, peer UDP port, and finite capture duration in seconds. It filters IPv4 UDP from `192.168.1.110` to `192.168.1.50` on `enp97s0`; zero independently disables either port filter, allowing attachment before the native flow is created. The launcher uses zero for both ports and retains actual ports on every RX line; offline analysis must select the admitted connection and reject unrelated UDP identities. It rejects IP fragments and packets whose protected prefix is not linear. It computes the exact identity scheme v2, including normalizing a zero lower-63-bit hash before adding the small-payload marker. It emits all nonempty payloads below 256 bytes and the same low-four-bits-zero sample of larger payloads. This is length selection, not a claim that every short packet contains an ACK.

Each `RX` line has nine values: `gro_monotonic_ns cpu signed_identity udp_payload_bytes napi_pointer poll_entry_monotonic_ns preceding_irq_monotonic_ns udp_source_port udp_destination_port`. The IRQ timestamp is snapshotted at poll entry. Zero NAPI/poll/IRQ values mean the associated context was not observed; do not substitute a previous packet or infer a missing value. Reject ambiguous repeated packet identities in offline joins.

The poll pointer and entry time associate a packet with the poll that processed it. The preceding IRQ is context, not a measured packet-arrival time: an interrupt may concern TX completion, the target packet may arrive during polling, and a poll can continue without a new interrupt. Therefore neither IRQ-to-GRO nor poll-to-GRO is automatically an exact per-packet residence to subtract from the BPF residual. This probe cannot measure hardware waiting before an interrupt.

The exit maps retain packet counts, missing-context and short-prefix counts, and distributions of poll duration, poll work, and preceding-IRQ-to-poll intervals. `napi:napi_poll` alone would provide a work result, not a poll-entry timestamp; this script uses typed function entry/exit instead. The script does not emit every IRQ or poll, and its context maps stay bounded by the NIC and CPU count. At 6,000 selected packets/second and roughly 120 bytes per text row, budget approximately 0.7 MB/second plus small exit summaries; this is an estimate, not measured observer overhead. The tracer still runs an entry hook at every matching IRQ/poll. Compare the existing control/observer evidence before treating any change as a transport effect.

All timestamps here use `CLOCK_MONOTONIC` via `nsecs(monotonic)`. The Go `monotime` values are relative to a process-specific origin, so do not directly subtract these absolute kernel timestamps from Go trace values. The IRQ/poll/GRO intervals within this trace need no clock conversion. An exact cross-trace absolute-time comparison would need a retained Linux clock calibration; identities alone do not supply one.

## Finite launcher

`helper.py` is executable through Python but remains inert until explicitly launched. It requires Linux root execution, a josh-owned private output directory directly below `/home/josh/wiremux-1460-diag-q1210-<run>`, and a josh-owned nonsymlink script inside that same staging root. It checks the exact compiled script SHA-256, rejects existing reserved outputs or a preexisting stop request, and reads the validated script into the child's stdin so later path replacement cannot change it.

The launcher runs the script with wildcard ports for 180 seconds and enforces a 195-second outer deadline. A regular `stop` marker requests SIGINT to its exact child; after three seconds it escalates to SIGTERM, then SIGKILL after five seconds. The outer deadline immediately kills a surviving child. Parent SIGINT, SIGTERM, and SIGHUP request the same cleanup. Linux process-local parent-death signals terminate the helper when its parent dies and kill the tracer if the helper dies unexpectedly. No process-name search or broad kill is used.

Artifacts are root-created mode 0644 in the private josh-owned directory, so josh can read and unlink them during normal staging cleanup. `custody.json` records both source hashes and exact argv. `ready.json` appears atomically only after the post-attach notification while the child is alive. `stdout.log` and `stderr.log` retain raw output. `result.json` records child exit status, END/readiness observations, stop reason, signal escalation, and warning count/examples. `capture_complete` requires successful exit, observed readiness and END, and no recorded stderr warnings or helper exception. Never release host ownership until the launcher exits. If the helper is forcibly killed, the tracer still receives its parent-death kill, but a complete final receipt is not promised.

Stage `helper.py` and `observe.bt` as josh, create the private output directory as josh, then use the existing operator to run this foreground command with its output retained:

```sh
mkdir -m 700 "$stage/napi-capture"
sudo -n python3 "$stage/helper.py" "$stage/napi-capture" "$stage/observe.bt"
```

The operator waits for `$stage/napi-capture/ready.json`, verifies the owned launcher/child are still alive, and then starts the native flow. To request an early stop, create only this marker as josh:

```sh
: > "$stage/napi-capture/stop"
```

Wait for the launcher to exit and read `$stage/napi-capture/result.json`; retain all artifacts before the existing staging cleanup. The launcher and script have not been started against the live datapath. `validate-helper.py` uses local fake subprocesses to test the path checks, readiness, stop-file, parent-signal, outer-deadline, and early-failure behavior without BPF or network traffic.

## Exact direct operator commands

First compile without attaching, substituting the staged script path and chosen port filters, where zero means wildcard (the duration is a positive finite number of seconds):

```sh
sudo -n bpftrace --mode codegen --verify-llvm-ir /absolute/staging/observe.bt "$local_port" "$peer_port" "$capture_seconds"
```

Launch this argv as an owned subprocess of the existing operator, with stdout and stderr retained separately:

```sh
sudo -n env __BPFTRACE_NOTIFY_PROBES_ATTACHED=1 bpftrace -B line -k /absolute/staging/observe.bt "$local_port" "$peer_port" "$capture_seconds"
```

Wait for a complete stdout line equal to `__BPFTRACE_NOTIFY_PROBES_ATTACHED`, and verify that the owned process is still running before releasing the traffic start. Do not use the script's `CONFIG` line or any `BEGIN` output as readiness. In bpftrace v0.25.0, BEGIN executes before ordinary probe attachment; the notification above is emitted after the attach loops. An attach failure or process exit before that marker means no admitted tracing capture. Do not use `--dry-run`: this version attaches probes before terminating.

The script exits itself on the first one-second tick after the supplied duration from BEGIN. To stop early, send SIGINT to the exact bpftrace PID owned by this operator; if launching through sudo, retain the child PID or let the operator's owned process-group handling forward the signal. Do not discover a target by broad process-name matching.

```sh
sudo -n kill -INT "$owned_bpftrace_pid"
```

Wait for that same owned process to exit and retain its exit status, both output streams, the `END` marker, all exit maps, and all lost-event/helper-read warnings. The `-k` option exposes probe-read/map-helper failures. Reject affected or missing joins; do not infer that successful compilation proves kernel-verifier acceptance, readiness, complete capture, or zero overhead.

## Source anchors

[bpftrace v0.25.0 compilation-only return before runtime](https://github.com/bpftrace/bpftrace/blob/v0.25.0/src/main.cpp#L1105), [BEGIN and post-attachment notification ordering](https://github.com/bpftrace/bpftrace/blob/v0.25.0/src/bpftrace.cpp#L507), [post-attachment readiness notification](https://github.com/bpftrace/bpftrace/blob/v0.25.0/src/bpftrace.cpp#L743), [dry-run and helper-warning documentation](https://github.com/bpftrace/bpftrace/blob/v0.25.0/man/adoc/bpftrace.adoc#L77).

## Optional operator adapter

`napi_adapter.py` defines `NapiCapture(op, docs, armname)` without modifying operator hooks or attaching probes. Keep `helper.py` and `observe.bt` beside the adapter when copying it to another directory. `stage()` freezes the three input files under `docs/napi-inputs-<arm>`, transfers helper/script under arm-specific names with `op.run`, and verifies their SHA-256, ownership, and mode remotely. Call it after the existing stage hook. The operator continues to own admission and all ordinary custody/cleanup.

A minimal import and hook composition, to be installed only by the owning operator, is:

```python
spec = importlib.util.spec_from_file_location('napi_adapter', candidate_dir / 'napi_adapter.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
napi = module.NapiCapture(op, docs, 'kernel')
old_stage, old_before, old_after, old_absence = op.stage, op.before_arm, op.after_arm, op.remote_absence

def stage(authorization):
    old_stage(authorization)
    napi.stage()

def before_arm(arm):
    old_before(arm)
    try:
        if arm['name'] == napi.arm:
            napi.start()
    except BaseException:
        old_after(arm)
        raise

def after_arm(arm=None):
    try:
        if napi.started and not napi.stopped:
            napi.stop()
    finally:
        old_after(arm)

def remote_absence():
    napi.remote_absence()
    old_absence()

op.stage, op.before_arm, op.after_arm, op.remote_absence = stage, before_arm, after_arm, remote_absence
```

`start()` creates the private remote capture directory, launches one owned SSH subprocess whose remote command runs the finite helper, retains launcher stdout/stderr and command receipts under `op.OPS`, and waits in one bounded remote readiness check for at most 45 seconds. It checks the exact helper and tracer argv, parent relationship, root ownership, and `/proc` start ticks before returning. A readiness failure attempts targeted cleanup before raising. `stop()` creates the stop marker and waits up to 15 seconds for the SSH command, then reads the result, checks source hashes and completeness, and proves the recorded process identities absent. `remote_absence()` also searches for the exact arm-specific helper argv and its matching tracer child when startup failed before PIDs were recorded; it never signals a process. PID reuse is reported separately and does not authorize touching the replacement process.

The adapter's exception cleanup only signals recorded identities with matching start ticks or a helper with the exact owned script/folder argv and its exact tracer child. Remote unreachability or surviving owned processes remain errors; the adapter does not claim custody cleanup succeeded. The caller must keep the existing failure/admission-release behavior. No hook integration or live start was performed during adapter preparation.

`validate-adapter.py` exercises the adapter through mocked SCP/SSH/process interfaces. Its retained `adapter-validation.json` covers successful stage/start/stop/absence, failed-readiness cleanup, incomplete-result rejection, live-process custody rejection, and invalid arm names. These checks verify local control flow; they do not claim a live NAPI attachment was tested.
