# D2 receive-storage experiment receipt

**Disposition:** Bounded no-change. Retain `TestDatagramReceiveRefillDrain`; do not ship the receive-ring substitution. This closes the experiment permitted by [D2](../adr/2026-09-06-datagram-plan.md#d2--reuse-receive-queue-storage), not a claim that the optimization cannot help on a quiet host.

## Versions and workload

- Comparison base: `2f22dedf99bfc36c204d1f61a3bce72b5d002539` (includes D1; not pure v0.62.0).
- Evaluated runtime candidate: `3f794277fe6987106cc8ba8397f80e226da94452`. The only runtime delta replaces receive slice storage with the existing lazy ring, under the existing lock. The final PR removes that delta.
- Toolchain: `go version go1.27.0 darwin/arm64`; Apple M4 Max, Darwin 25.6.0; `GOMAXPROCS=2` for both versions.
- Analysis: `golang.org/x/perf/cmd/benchstat@v0.0.0-20260825160852-19be9d8e6c70`, default comparisons.
- Unchanged `BenchmarkDatagramReceive` cases use 1071-byte frames, disabled debug logging, 10 samples at 200 ms. A complete capacity fill and drain happens before `b.Loop`, excluding initial growth from warmed reuse. SteadyDrain is one admission/removal per operation; BurstDrain is 128 admissions/removals; Overflow is one attempted drop at full occupancy.

Initial pair command on each version: `GOMAXPROCS=2 go test -run '^$' -bench '^BenchmarkDatagramReceive' -benchmem -count=10 -benchtime=200ms .`. The base includes only the additional characterization test, excluded by `-run '^$'`; runtime and benchmark source match the comparison base. The replacement pair used `go test -c` binaries built from the exact base and candidate with equivalent `-test.*` flags, base first then candidate. No third pair or parameter sweep was run.

## Results and decision

| Case | Initial ns/op, base → candidate | Replacement ns/op, base → candidate | B/op, base → candidate | allocs/op, base → candidate |
| --- | --- | --- | --- | --- |
| SteadyDrain | 118.8 → 144.3 (+21.46%, p=0.000) | 131.45 → 126.55 (not significant, p=0.481) | 1175 → 1152 | 1 → 1 |
| BurstDrain (128 messages) | 15205.5 → 19973.5 (+31.36%, p=0.000) | 17793.5 → 16289.5 (-8.45%, p=0.004) | 153600 → 147456 | 130 → 128 |
| Overflow | 2.928 → 2.956 (+0.96%, p=0.000) | 2.958 → 2.9455 (not significant, p=0.591) | 0 → 0 | 0 → 0 |

Values are sample medians (Go allocation counters are integer-per-operation reports). Allocation bytes consistently decline: the candidate retains only the necessary 1152-byte allocator size class for each admitted 1071-byte payload, and removes two metadata allocations per warmed burst. SteadyDrain's integer allocation count masks fractional metadata churn; its byte counter reports that difference. These are metadata-cost observations, not application throughput, universal heap-size, or unbounded-leak claims.

The replacement was admitted solely for documented host contention. Process snapshots after the first pair and bracketing the replacement showed unrelated `gridcast.test`/`transferstate.test` test processes, another project's benchmark runner, and filesystem indexing activity. The unrelated runner reached 102.7% CPU at replacement completion, while `fseventsd` remained above 100%. No unrelated processes were stopped. SteadyDrain's replacement sample uncertainty remained ±12% on base and ±19% on candidate, and the initial substantial slowdown reversed direction. The permitted replacement did not resolve contamination or establish a trustworthy timing tradeoff. Under the plan's finite evidence policy, retain no runtime change and do not tune thresholds or request another run. This is an inconclusive timing disposition, not a demonstrated reproducible ring slowdown.

## Preservation evidence and boundary

`TestDatagramReceiveRefillDrain` passed before and after the experimental substitution. Its 16 cycles admit distinct payloads to capacity, drain 17, refill 18 (dropping the overflow), check the full FIFO sequence, and verify emptiness through a canceled context. Existing D1 input/returned-payload ownership, empty frame, overflow-allocation, concurrent producer/drainer, blocking, cancellation and close tests are retained without duplication.

The experimental candidate passed `go test -count=1 -run 'TestDatagram' .`, `go test -race -count=1 -run 'TestDatagram' .`, and `go test -count=1 ./internal/utils/ringbuffer`. Source inspection confirmed that existing `RingBuffer.PopFront` clears its removed slot; no shared ring edit or GC timing test was introduced. Final-head review and certification receipts belong in the PR discussion.

The final change consists of one ordinary verification aid plus process metadata. No new maintained verification tool, closure matrix, authority boundary, platform matrix, stress campaign, or follow-up requirement is introduced. Runtime, API, wire behavior, lock topology, queue limits and caller ownership match the comparison base. No production effect remains untraced. The product PR owns the committed D2 completion and H1-only frontier transition.

## Raw samples

### base

```text
goos: darwin
goarch: arm64
pkg: github.com/quic-go/quic-go
cpu: Apple M4 Max
BenchmarkDatagramReceive/SteadyDrain-2         	 2033649	       118.5 ns/op	    1176 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 2060030	       119.9 ns/op	    1175 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1817371	       133.7 ns/op	    1175 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 2095053	       114.8 ns/op	    1175 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1946077	       120.5 ns/op	    1175 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 2037199	       115.8 ns/op	    1175 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 2050626	       115.4 ns/op	    1175 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1925120	       121.3 ns/op	    1175 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1949372	       119.1 ns/op	    1176 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 2053786	       118.1 ns/op	    1175 B/op	       1 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   15969	     15119 ns/op	  153600 B/op	     130 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   15088	     15698 ns/op	  153600 B/op	     130 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   15754	     15253 ns/op	  153600 B/op	     130 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   16112	     14918 ns/op	  153600 B/op	     130 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   16698	     14479 ns/op	  153600 B/op	     130 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   16190	     15103 ns/op	  153601 B/op	     130 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   15673	     15455 ns/op	  153600 B/op	     130 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   15506	     15459 ns/op	  153600 B/op	     130 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   16064	     15158 ns/op	  153600 B/op	     130 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   15474	     15889 ns/op	  153600 B/op	     130 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81484815	         2.943 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81297369	         2.931 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81267529	         2.928 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81740388	         2.935 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	82061802	         2.912 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81384678	         2.923 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81582925	         2.942 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81077656	         2.928 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81146188	         2.922 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81920572	         2.927 ns/op	       0 B/op	       0 allocs/op
PASS
ok  	github.com/quic-go/quic-go	7.493s
```

### candidate

```text
goos: darwin
goarch: arm64
pkg: github.com/quic-go/quic-go
cpu: Apple M4 Max
BenchmarkDatagramReceive/SteadyDrain-2         	 1515364	       167.6 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1307442	       179.2 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1584889	       146.5 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1625901	       148.6 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1808886	       137.2 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1702234	       140.3 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1674852	       148.2 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1729179	       139.8 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1623716	       142.1 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1653243	       141.7 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   12928	     18584 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   13440	     18234 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   12008	     20499 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   10000	     20649 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   10000	     25091 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   10000	     20264 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   12219	     19805 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   10000	     20142 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   12800	     18642 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   12562	     19223 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/Overflow-2            	80741230	         2.952 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	78463424	         2.956 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81219428	         2.979 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	80618071	         2.956 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81372040	         2.958 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81282474	         2.951 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81631514	         2.954 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81361668	         2.974 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81040012	         2.958 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	80612412	         2.951 ns/op	       0 B/op	       0 allocs/op
PASS
ok  	github.com/quic-go/quic-go	7.568s
```

### base-replacement

```text
goos: darwin
goarch: arm64
pkg: github.com/quic-go/quic-go
cpu: Apple M4 Max
BenchmarkDatagramReceive/SteadyDrain-2         	 1780672	       131.5 ns/op	    1176 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1373474	       173.8 ns/op	    1176 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1696578	       143.7 ns/op	    1175 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1871696	       146.9 ns/op	    1175 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1822840	       134.2 ns/op	    1175 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1721149	       131.4 ns/op	    1175 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1953174	       120.7 ns/op	    1175 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1886208	       129.0 ns/op	    1175 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1854628	       126.3 ns/op	    1175 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1959271	       121.2 ns/op	    1175 B/op	       1 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   13389	     18493 ns/op	  153600 B/op	     130 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   12948	     19415 ns/op	  153600 B/op	     130 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   10000	     21419 ns/op	  153601 B/op	     130 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   13804	     18117 ns/op	  153600 B/op	     130 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   14404	     19763 ns/op	  153600 B/op	     130 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   14277	     17293 ns/op	  153600 B/op	     130 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   13624	     17470 ns/op	  153600 B/op	     130 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   14698	     16165 ns/op	  153600 B/op	     130 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   14600	     16607 ns/op	  153600 B/op	     130 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   14506	     16617 ns/op	  153601 B/op	     130 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81772890	         2.911 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	82011552	         2.958 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	72745634	         3.179 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81757791	         2.958 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81206814	         2.966 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81149619	         2.948 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	80962563	         2.967 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81610695	         2.962 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	82161307	         2.929 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	82160126	         2.932 ns/op	       0 B/op	       0 allocs/op
PASS
```

### candidate-replacement

```text
goos: darwin
goarch: arm64
pkg: github.com/quic-go/quic-go
cpu: Apple M4 Max
BenchmarkDatagramReceive/SteadyDrain-2         	 1925394	       124.3 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 2016050	       122.5 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1805782	       133.1 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1969976	       124.1 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 2012114	       123.9 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1851948	       125.2 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 2064874	       128.6 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1859558	       150.5 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1791110	       127.9 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/SteadyDrain-2         	 1583496	       161.3 ns/op	    1152 B/op	       1 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   14116	     17429 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   14581	     16444 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   13671	     17297 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   15567	     15818 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   15043	     16043 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   15574	     15553 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   14463	     16455 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   15016	     16135 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   15116	     15884 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/BurstDrain-2          	   13700	     17131 ns/op	  147456 B/op	     128 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81063963	         2.962 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	82031397	         2.961 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81264116	         2.948 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	79831474	         2.976 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	81300812	         2.955 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	77628022	         2.941 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	80971659	         2.939 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	82416393	         2.936 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	80486270	         2.943 ns/op	       0 B/op	       0 allocs/op
BenchmarkDatagramReceive/Overflow-2            	82163642	         2.929 ns/op	       0 B/op	       0 allocs/op
PASS
```
