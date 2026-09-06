# D2 Linux follow-up protocol

The user authorized this follow-up after D2 closed: retest the existing ring on minimax with controlled, interleaved measurements; consider a head-index slice only if the ring's tradeoff remains unattractive. This is an experiment, not authorization to rewrite D2's completed contract or merge a runtime change. The comparison starts at `686bce6541438101c96732a023984e031faf6df7`.

## Question and candidates

Does adopting the existing lazy receive ring reduce metadata costs without a material timing regression on minimax? The baseline retains the current slice queue. The candidate changes only receive storage to the existing `RingBuffer[[]byte]`, with the same lock, queue limit, payload copies, close/cancel behavior and notification path. Shared ring code, send behavior, dependencies and public APIs remain unchanged.

The common experimental benchmark and immutable source archives are verification aids for this finite investigation, not maintained product deliverables. The report, protocol and sample records are evidence metadata. No new ownership or persistence contract is introduced. Existing focused tests and the inherited ring tests cover the implementation seam; no recursive harness assurance or new closure matrix is required.

## Measurement plan fixed before collection

- Host: minimax, Ryzen AI MAX+ 395, Linux/amd64. Build both native binaries with the same installed Go 1.26.0, identical flags, benchmark source and dependencies. Record source commits, archive/binary hashes, toolchain and host details.
- CPU affinity: logical CPUs 12 and 13, distinct physical cores; monitor their siblings 28 and 29. Set `GOMAXPROCS=2`. The precheck found these cores and siblings idle. Keep the existing amd-pstate-epp/powersave policy unchanged, record it, and monitor activity; affinity is not exclusive CPU reservation.
- Existing warmed workloads: one 1071-byte admission/removal, a 128-message burst/drain, and one attempted drop at full capacity. Add one warmed unpaced producer/receiver workload using separate goroutines; count delivered messages and drops, include final drain/join time, and report useful delivery rate. Default Go allocation/time metrics in that concurrent case are per offered message, not per delivered message.
- Run one fixed set: 20 paired rounds, one second per version per workload, 80 samples per version. Alternate baseline/candidate order each round and rotate workload order. Precompile both binaries before collection. No concurrent builds or agent tasks on the selected cores are started by this experiment. Fixed-count smoke checks validate execution/accounting before collection and are excluded from the performance data.
- Analyze both byte/allocation counters and timing. Report benchstat medians and comparisons, plus a fixed-seed paired bootstrap interval for the geometric mean candidate/base ratio using 10,000 resamples of the 20 paired rounds. The bootstrap describes this sample set, not a universal timing guarantee. Use ns/op for sequential workloads and ns/delivered for the concurrent workload; report its drop fraction alongside it.
- A candidate is promising for a separately reviewed implementation when metadata savings persist and each primary timing ratio's 95% interval is below 1.05, with no material correctness or delivery-accounting concern. A clear slowdown beyond that budget is unattractive; an interval crossing the budget or material host contamination remains inconclusive. Lack of statistical significance is not evidence of equivalence. No repeated testing until significance or selective deletion of slow samples.
- Do not interpret microbenchmark delivery rate as application/file/network throughput. One producer and one receiver characterize this workload; they do not narrow the supported caller contract to SPSC.

## Termination and next decision

Run focused datagram tests, their race variant, and ring tests against both source versions; smoke-check the concurrent benchmark under the race detector. Complete the one fixed collection and report its result. If the existing ring is promising, stop architecture exploration and recommend its separate implementation gate. If it is unattractive or inconclusive, decide whether the head-index slice can answer a distinct question with a bounded second experiment before building it. A fixed-capacity ring, synchronization replacement, payload pooling, physical-network run and broader parameter sweep are outside this experiment.

Production `main`, D2's completed issue/task, and its historical receipt remain unchanged. Keep the experimental code and evidence on dedicated feature branches/worktrees until the result has been reviewed or absorbed into subsequent work.
