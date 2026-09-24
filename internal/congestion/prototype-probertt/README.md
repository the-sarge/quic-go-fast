# Disposable ProbeRTT comparison

**PROTOTYPE — do not import into the transport or merge this terminal shell into production.** This model asks how the pinned draft, filtering proposal and QUICHE ProbeRTT subset differ on queued, changing-RTT, delayed-feedback and genuinely idle schedules. It isolates the question in a pure state reducer plus a deterministic packet/ACK harness; it is not an implementation of BBRv3.

From the repository root, run:

```sh
go run ./internal/congestion/prototype-probertt -scenario expired-queue
```

The terminal displays three controller snapshots. Press `n` then Enter for the next recorded event, `s` for one second, `r` to restart and `q` to quit. Snapshots retain their own event timestamps; they are not falsely interpolated across ACK gaps. The cap displayed outside ProbeRTT is hypothetical. The saved cap and second filter are meaningful only for the variants that use them.

Available scenarios: `ordinary-queue`, `expired-queue`, `expired-no-backlog`, `rising-base`, `long-rtt`, `delayed-feedback`, `idle-restart`, `late-down-exit`, `falling-base`. Choose another with `-scenario`. `-mode summary -scenario all` prints the campaign, `-mode events -scenario all` prints transition traces, and `-mode csv -scenario all` emits snapshots and every minimum change. Commands write only to standard output; the viewer keeps state in memory.

Read the [findings and modeling limits](../../../docs/audits/2026-09-23-probertt-prototype/README.md) before interpreting the numbers. The [source correspondence](SOURCES.md) and [third-party notices](THIRD_PARTY_NOTICES) identify the translated algorithms. `model.go` owns the reducer and simulation; `main.go` only selects and displays runs.

Owner disposition is pending. After the owner reacts, preserve the answer and selected evidence on the investigation ticket, then delete this shell or retain this branch solely as a frozen, explicitly disposable investigation asset. Do not promote its packet harness or simplified cwnd behavior into the controller implementation.
