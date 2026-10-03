#!/usr/bin/env python3
"""Semantic mutation sweep for the bounded-work BBR ECN feedback rewrite (#709).

Each mutant replaces exactly one occurrence in internal/ackhandler/bbr_ecn.go,
runs the ECN tests (oracle, named, cap, work and allocation tests), and
restores the file. Run from the repository root:

    python3 docs/audits/2026-10-02-bbr-ecn-bounded-feedback/mutate.py
"""
import pathlib, subprocess, sys, time

SRC = pathlib.Path("internal/ackhandler/bbr_ecn.go")
TESTS = ["go", "test", "./internal/ackhandler/", "-run", "ECN", "-count=1", "-failfast", "-timeout", "300s"]

SCRATCH_MERGE = "extendsRange(&e.scratch[n-1], &r)"
PRED = "(prev.last+1 == r.first && prev.mark == r.mark && prev.generation == r.generation && prev.acked == r.acked && prev.ordinal+uint64(r.first-prev.first) == r.ordinal)"
def scratch_pred(drop):
    p = PRED.replace("prev.", "e.scratch[n-1].")
    p = p.replace(drop, "true")
    return p

MUTANTS = [
    # window bounds
    ("W1", "start searches from LargestAcked", "e.searchRanges(ack.LowestAcked())", "e.searchRanges(largest)"),
    ("W2", "end searches records ending at LargestAcked", "e.searchRanges(largest+1)", "e.searchRanges(largest)"),
    ("W3", "anchor-containing record not added to window end", "\t\tend++\n", "\t\t_ = end\n"),
    ("W4", "end adjustment uses strict first < largest", "e.ranges[end].first <= largest", "e.ranges[end].first < largest"),
    # neighbour extension
    ("N1", "no left neighbour", "max(start-1, 0)", "max(start, 0)"),
    ("N2", "no right neighbour", "min(end+1, len(e.ranges))", "min(end, len(e.ranges))"),
    ("N3", "no neighbours", "start, end = max(start-1, 0), min(end+1, len(e.ranges))", "start, end = max(start, 0), min(end, len(e.ranges))"),
    ("N4", "two left neighbours", "max(start-1, 0)", "max(start-2, 0)"),
    # merge cursor
    ("C1", "skip ACK ranges ending at the record start", "ack.AckRanges[j].Largest < r.first", "ack.AckRanges[j].Largest <= r.first"),
    ("C2", "inner loop stops before a range starting at the record end", "ack.AckRanges[i].Smallest <= r.last", "ack.AckRanges[i].Smallest < r.last"),
    ("C3", "inner loop restarts at the lowest ACK range", "for i := j; i >= 0", "for i := len(ack.AckRanges) - 1; i >= 0"),
    ("C4", "skip loop never advances", "\t\t\tj--\n", "\t\t\tbreak\n"),
    ("C5", "anchor taken from the first intersecting packet", "if last == largest {", "if first == largest {"),
    ("C6", "anchor uses the record start", "anchor = r.ordinal + uint64(last-r.first)", "anchor = r.ordinal"),
    ("C7", "already-acked records counted as new", "\t\t\tif !r.acked {\n\t\t\t\tn :=", "\t\t\tif true {\n\t\t\t\tn :="),
    ("C8", "intersection length off by one", "n := uint64(last - first + 1)", "n := uint64(last - first)"),
    ("C9", "ECT1 counted as ECT0", "\t\t\t\tif r.mark == protocol.ECT1 {\n\t\t\t\t\tnewly.ECT1 += n", "\t\t\t\tif r.mark == protocol.ECT1 {\n\t\t\t\t\tnewly.ECT0 += n"),
    ("C10", "gap part ordinal from the ACK start", "\t\t\t\tpart.ordinal += uint64(cursor - r.first)\n\t\t\t\te.appendScratch(part)", "\t\t\t\tpart.ordinal += uint64(first - r.first)\n\t\t\t\te.appendScratch(part)"),
    ("C11", "acked part ordinal from the cursor", "\t\t\tpart.ordinal += uint64(first - r.first)\n\t\t\tpart.acked = true", "\t\t\tpart.ordinal += uint64(cursor - r.first)\n\t\t\tpart.acked = true"),
    ("C12", "cursor does not pass the acked part", "cursor = last + 1", "cursor = last"),
    ("C13", "tail dropped when one packet remains", "if cursor <= r.last {", "if cursor < r.last {"),
    ("C14", "empty gap part emitted", "if cursor < first {", "if cursor <= first {"),
    ("C15", "gap part ends at the ACK start", "\t\t\t\tpart.last = first - 1", "\t\t\t\tpart.last = first"),
    ("C16", "acked part not marked acked", "\t\t\tpart.acked = true\n", "\t\t\tpart.acked = r.acked\n"),
    ("C17", "scratch not reset per ACK", "\te.scratch = e.scratch[:0]\n", "\t_ = e.scratch\n"),
    # merge predicate (scratch call site only; registration keeps the real predicate)
    ("P1", "scratch merge ignores contiguity", SCRATCH_MERGE, scratch_pred("e.scratch[n-1].last+1 == r.first")),
    ("P2", "scratch merge ignores the mark", SCRATCH_MERGE, scratch_pred("e.scratch[n-1].mark == r.mark")),
    ("P3", "scratch merge ignores the generation", SCRATCH_MERGE, scratch_pred("e.scratch[n-1].generation == r.generation")),
    ("P4", "scratch merge ignores ACK status", SCRATCH_MERGE, scratch_pred("e.scratch[n-1].acked == r.acked")),
    ("P5", "scratch merge ignores ordinal affinity", SCRATCH_MERGE, scratch_pred("e.scratch[n-1].ordinal+uint64(r.first-e.scratch[n-1].first) == r.ordinal")),
    ("P6", "scratch never merges", SCRATCH_MERGE, "false"),
    # projected-length check and validation order
    ("L1", "budget allows one record too many", "+len(e.scratch) > maxECNMarkRanges {", "+len(e.scratch) > maxECNMarkRanges+1 {"),
    ("L2", "budget rejects a full ledger", "+len(e.scratch) > maxECNMarkRanges {", "+len(e.scratch) >= maxECNMarkRanges {"),
    ("L3", "budget charges the whole window as growth", "len(e.ranges)-(end-start)+len(e.scratch) > maxECNMarkRanges", "len(e.ranges)+len(e.scratch) > maxECNMarkRanges"),
    ("L4", "no budget check", "len(e.ranges)-(end-start)+len(e.scratch) > maxECNMarkRanges", "false"),
    ("L5", "budget failure records counter failure", "\tif len(e.ranges)-(end-start)+len(e.scratch) > maxECNMarkRanges {\n\t\te.failEvidence()", "\tif len(e.ranges)-(end-start)+len(e.scratch) > maxECNMarkRanges {\n\t\te.failCounters()"),
    ("L6", "budget checked before counter validation", "\tif counts.ECT0 < e.accepted.ECT0", "\tif len(e.ranges)-(end-start)+len(e.scratch) > maxECNMarkRanges {\n\t\te.failEvidence()\n\t\tresult.Failed = true\n\t\treturn result\n\t}\n\tif counts.ECT0 < e.accepted.ECT0"),
    ("L8", "budget checked between anchor and delta coverage", "\tdelta := congestion.ECNCounts{", "\tif len(e.ranges)-(end-start)+len(e.scratch) > maxECNMarkRanges {\n\t\te.failEvidence()\n\t\tresult.Failed = true\n\t\treturn result\n\t}\n\tdelta := congestion.ECNCounts{"),
    ("L7", "splice before validation (not atomic)", "\tif counts.ECT0 < e.accepted.ECT0", "\te.splice(start, end)\n\tif counts.ECT0 < e.accepted.ECT0"),
    # splice
    ("S1", "suffix not relocated", "\tcopy(e.ranges[start+len(e.scratch):], e.ranges[end:old])\n", ""),
    ("S2", "window not written", "\tcopy(e.ranges[start:], e.scratch)\n", ""),
    ("S3", "ledger not truncated after shrinking", "\te.ranges = e.ranges[:n]\n}", "\t_ = n\n}"),
    ("S4", "suffix relocated one slot early", "copy(e.ranges[start+len(e.scratch):], e.ranges[end:old])", "copy(e.ranges[max(start+len(e.scratch)-1, 0):], e.ranges[end:old])"),
    ("S5", "growth ignores the cap clamp", "min(maxECNMarkRanges, max(n, 2*cap(e.ranges)))", "max(n, 2*cap(e.ranges))"),
    ("S6", "grows on every splice", "if n > cap(e.ranges) {", "if n > 0 {"),
]

def main():
    original = SRC.read_text()
    results = []
    try:
        for mid, desc, old, new in MUTANTS:
            count = original.count(old)
            if count != 1:
                print(f"{mid}: target occurs {count} times", file=sys.stderr)
                sys.exit(2)
            SRC.write_text(original.replace(old, new))
            t0 = time.time()
            p = subprocess.run(TESTS, capture_output=True, text=True)
            out = p.stdout + p.stderr
            if p.returncode == 0:
                verdict, why = "SURVIVED", ""
            elif "build failed" in out or "[build failed]" in out or "setup failed" in out:
                verdict, why = "BUILD", out.strip().splitlines()[0]
            else:
                verdict = "KILLED"
                fails = [l.strip() for l in out.splitlines() if l.strip().startswith("--- FAIL")]
                why = fails[-1] if fails else (out.strip().splitlines() or ["?"])[-1]
            dt = time.time() - t0
            line = f"{mid}\t{verdict}\t{dt:5.1f}s\t{desc}\t{why}"
            print(line, flush=True)
            results.append(line)
    finally:
        SRC.write_text(original)
    killed = sum(" KILLED" in "\t" + l.replace("\t", " ") for l in results)
    print(f"\n{killed}/{len(results)} killed")

if __name__ == "__main__":
    main()
