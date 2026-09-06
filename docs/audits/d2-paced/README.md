# Paced receive-queue evidence

The [protocol](../2026-09-06-d2-paced-protocol.md) fixes the workload, collection, interpretation, and retirement boundary. The [results](../2026-09-06-d2-paced-results.md) explain the outcome. All artifacts in this directory are experimental evidence; the runtime candidate is preserved as a patch and on its own branch, not applied to the evidence branch.

## Source and collection

Frozen baseline: `37914bf7e884ff5f777d9ef9f3df32ed1e68fa7a`. Frozen head-index candidate: `5d3fdb0170dc58ab6bfa871e32cc9eb925fcf4aa`. Export each with `git archive`, extract into `base` and `head-index` directories under a fresh experiment root on minimax, and compare archive checksums against `source-hashes.log`. `source-diff.log` records the extracted-tree comparison. The only differing file is `datagram_queue.go`.

Build each extracted tree natively with `GOTOOLCHAIN=local go test -tags=queue_experiment -c -o ../base.test .` (use `../head-index.test` for the candidate). `binary-hashes.log`, `toolchain.log`, `host.log`, and `validation.log` preserve the measured builds and validation. To collect a new explicitly authorized experiment, use a fresh root containing those two binaries and run `python3 base/docs/audits/d2-paced/collect.py /absolute/experiment/root`. The collector requires `taskset` and `mpstat`; CPU choices and sample budget are intentionally fixed in its source. It refuses an existing manifest.

A single diagnostic invocation uses the following environment and command. It is not a replacement for the interleaved collection, and a race-enabled invocation is correctness evidence only.

```sh
QUEUE_EXPERIMENT=1 QUEUE_RATE=100000 QUEUE_BURST=32 QUEUE_STALL=1 QUEUE_DURATION_MS=2000 GOMAXPROCS=2 taskset -c 12,13 ./base.test -test.run '^TestDatagramReceivePacedExperiment$' -test.v -test.count=1 -test.timeout=15s
```

## Analysis

`samples.jsonl.gz` contains every original command, explicit experiment environment, timing, frequency snapshot, return code, stdout, and stderr. It is a deterministic gzip copy of the complete one-shot manifest; no measurement samples are omitted. `cpu-activity.log` records selected cores and their SMT siblings throughout collection; `precheck.log` records their activity immediately beforehand. Analysis uses only the Python standard library; figure rendering additionally uses Matplotlib.

```sh
python3 docs/audits/d2-paced/analyze.py docs/audits/d2-paced/samples.jsonl.gz > /tmp/d2-paced-analysis.json
diff /tmp/d2-paced-analysis.json docs/audits/d2-paced/analysis.json
python3 docs/audits/d2-paced/plot.py docs/audits/d2-paced
```

The analyzer checks the complete case/round/variant matrix, fixed duration, successful results, FIFO/integrity checks enforced by the harness, and both accounting identities. It retains rate misses and identifies their exact rounds. The plotted points are paired estimates, not differences between the independently reported variant medians. CPU includes the busy pacer; latency includes the enqueue attempt, wakeup/scheduling, and common measurement work; receiver pauses are requested sleeps whose actual lengths are recorded. This evidence does not isolate transport CPU or establish end-to-end transfer speed.
